#!/usr/bin/env python3
"""
Pensieve online inference service for GoDASH.

This service keeps the original Pensieve architecture boundary:
- the player collects runtime state after each chunk
- the external ABR server maintains the S_INFO x S_LEN state tensor
- the server runs the pretrained actor network and returns the next bitrate index

The network structure is adapted from hongzimao/pensieve (MIT licensed),
but this service accepts next_chunk_sizes from the client so it can be used
with GoDASH datasets that already expose per-chunk size metadata.
"""

import argparse
import json
import os
import random
from http.server import BaseHTTPRequestHandler, HTTPServer

import numpy as np
import tensorflow.compat.v1 as tf
import tflearn

tf.disable_v2_behavior()
os.environ.setdefault("CUDA_VISIBLE_DEVICES", "")

S_INFO = 6
S_LEN = 8
A_DIM = 6
BUFFER_NORM_FACTOR = 10.0
CHUNK_TIL_VIDEO_END_CAP = 48.0
M_IN_K = 1000.0
RAND_RANGE = 1000


class ActorNetwork(object):
    def __init__(self, sess, state_dim, action_dim):
        self.sess = sess
        self.s_dim = state_dim
        self.a_dim = action_dim
        self.inputs, self.out = self.create_actor_network()

    def create_actor_network(self):
        with tf.variable_scope("actor"):
            inputs = tflearn.input_data(shape=[None, self.s_dim[0], self.s_dim[1]])
            split_0 = tflearn.fully_connected(inputs[:, 0:1, -1], 128, activation="relu")
            split_1 = tflearn.fully_connected(inputs[:, 1:2, -1], 128, activation="relu")
            split_2 = tflearn.conv_1d(inputs[:, 2:3, :], 128, 4, activation="relu")
            split_3 = tflearn.conv_1d(inputs[:, 3:4, :], 128, 4, activation="relu")
            split_4 = tflearn.conv_1d(inputs[:, 4:5, :A_DIM], 128, 4, activation="relu")
            split_5 = tflearn.fully_connected(inputs[:, 5:6, -1], 128, activation="relu")
            split_2_flat = tflearn.flatten(split_2)
            split_3_flat = tflearn.flatten(split_3)
            split_4_flat = tflearn.flatten(split_4)
            merge_net = tflearn.merge([split_0, split_1, split_2_flat, split_3_flat, split_4_flat, split_5], "concat")
            dense_net_0 = tflearn.fully_connected(merge_net, 128, activation="relu")
            out = tflearn.fully_connected(dense_net_0, self.a_dim, activation="softmax")
            return inputs, out

    def predict(self, inputs):
        return self.sess.run(self.out, feed_dict={self.inputs: inputs})


class PensieveSession(object):
    def __init__(self):
        self.reset()

    def reset(self):
        self.state = np.zeros((S_INFO, S_LEN), dtype=np.float32)
        self.last_total_rebuf = 0.0


def choose_action(action_prob, deterministic=False):
    if deterministic:
        return int(np.argmax(action_prob))
    action_cumsum = np.cumsum(action_prob)
    threshold = random.randint(1, RAND_RANGE) / float(RAND_RANGE)
    return int((action_cumsum > threshold).argmax())


def build_state(prev_state, payload):
    state = np.array(prev_state, copy=True)
    state = np.roll(state, -1, axis=1)

    video_bitrates = payload["videoBitRate"]
    lastquality = int(payload["lastquality"])
    buffer_seconds = float(payload["buffer"])
    fetch_time_ms = float(payload["lastChunkFinishTime"]) - float(payload["lastChunkStartTime"])
    if fetch_time_ms <= 0:
        raise ValueError("lastChunkFinishTime must be larger than lastChunkStartTime")

    last_chunk_size = float(payload["lastChunkSize"])
    next_chunk_sizes = payload["nextChunkSizes"]
    remain = float(payload["video_chunk_remain"])

    state[0, -1] = video_bitrates[lastquality] / float(max(video_bitrates))
    state[1, -1] = buffer_seconds / BUFFER_NORM_FACTOR
    state[2, -1] = last_chunk_size / fetch_time_ms / M_IN_K
    state[3, -1] = fetch_time_ms / M_IN_K / BUFFER_NORM_FACTOR
    state[4, :A_DIM] = np.array(next_chunk_sizes, dtype=np.float32) / M_IN_K / M_IN_K
    state[5, -1] = min(remain, CHUNK_TIL_VIDEO_END_CAP) / CHUNK_TIL_VIDEO_END_CAP
    return state


def make_handler(actor, session, deterministic=False):
    class Handler(BaseHTTPRequestHandler):
        def _write_text(self, status_code, text):
            body = text.encode("utf-8")
            self.send_response(status_code)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Access-Control-Allow-Origin", "*")
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            if self.path == "/reset":
                session.reset()
                self._write_text(200, "ok")
                return

            if self.path != "/predict":
                self._write_text(404, "unknown endpoint")
                return

            length = int(self.headers.get("Content-Length", "0"))
            payload = json.loads(self.rfile.read(length))

            required = [
                "lastquality",
                "buffer",
                "RebufferTime",
                "lastChunkFinishTime",
                "lastChunkStartTime",
                "lastChunkSize",
                "nextChunkSizes",
                "video_chunk_remain",
                "videoBitRate",
            ]
            missing = [field for field in required if field not in payload]
            if missing:
                self._write_text(400, "missing fields: " + ",".join(missing))
                return

            if len(payload["nextChunkSizes"]) != A_DIM or len(payload["videoBitRate"]) != A_DIM:
                self._write_text(400, "Pensieve pretrained actor expects exactly 6 actions")
                return

            try:
                state = build_state(session.state, payload)
            except Exception as exc:
                self._write_text(400, str(exc))
                return

            action_prob = actor.predict(np.reshape(state, (1, S_INFO, S_LEN)))[0]
            action = choose_action(action_prob, deterministic=deterministic)
            session.state = state
            session.last_total_rebuf = float(payload["RebufferTime"])
            self._write_text(200, str(action))

        def log_message(self, _format, *_args):
            return

    return Handler


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8333)
    parser.add_argument("--model", required=True, help="Path to a pretrained Pensieve checkpoint, e.g. pretrain_linear_reward.ckpt")
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--deterministic", action="store_true", help="Use argmax instead of stochastic action sampling")
    return parser.parse_args()


def main():
    args = parse_args()
    random.seed(args.seed)
    np.random.seed(args.seed)
    tf.set_random_seed(args.seed)

    session_state = PensieveSession()
    with tf.Session() as sess:
        actor = ActorNetwork(sess, state_dim=[S_INFO, S_LEN], action_dim=A_DIM)
        sess.run(tf.global_variables_initializer())
        saver = tf.train.Saver()
        saver.restore(sess, args.model)

        server = HTTPServer((args.host, args.port), make_handler(actor, session_state, deterministic=args.deterministic))
        print("Pensieve service listening on http://%s:%d using model %s" % (args.host, args.port, args.model))
        server.serve_forever()


if __name__ == "__main__":
    main()
