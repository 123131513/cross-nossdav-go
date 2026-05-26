# Pensieve Python 3.8 环境建立与服务启动说明

本文档记录如何在当前仓库中从零建立 Pensieve 所需的 Python 环境，并启动
`paper-utilities/pensieve/rl_server/rl_server_no_training.py` 服务。

适合第一次配置的同学按顺序执行。所有命令默认在当前仓库根目录执行：

```bash
cd /home/quic2/masque-HAS/cross-nossdav-go
```

## 1. 目标

最终要得到：

- 一个不污染系统 Python 的独立 Python 3.8 环境
- 能导入 `tensorflow==2.7.0` 和 `tflearn==0.5.0`
- Pensieve `rl_server_no_training.py` 能恢复 checkpoint
- 本机 `http://127.0.0.1:8333` 能返回码率动作

当前主机没有 GPU 不影响在线推理。TensorFlow 启动时打印的 CUDA 相关警告可以忽略。

## 2. 为什么不用系统 Python

当前主机上的系统 Python 是：

```bash
python3 --version
```

实际结果为：

```text
Python 3.10.12
```

Pensieve 这版服务依赖旧版 TensorFlow/TFLearn 组合，更稳妥的运行环境是：

- Python 3.8
- TensorFlow 2.7.0
- TFLearn 0.5.0
- protobuf 3.20.x
- Pillow 9.x

另外，当前系统的 `python3 -m venv` 依赖 `ensurepip`，本机没有完整安装 `python3.10-venv`，直接创建 venv 会失败。因此本文使用仓库内的 Miniforge/conda 环境。

## 3. 环境目录约定

本文把工具和 Python 环境都放在当前仓库的 `.tools/` 目录下：

```text
.tools/miniforge3-pensieve-local/   # Miniforge/conda 本体
.tools/pensieve-py38/               # Pensieve 专用 Python 3.8 环境
```

`.tools/` 已加入 `.gitignore`，不会被提交到 Git。

## 4. 安装 Miniforge

下载安装脚本：

```bash
curl -L -o /tmp/Miniforge3-pensieve.sh \
  https://github.com/conda-forge/miniforge/releases/latest/download/Miniforge3-Linux-x86_64.sh
```

创建临时 HOME，避免安装器写入真实用户目录：

```bash
mkdir -p /tmp/pensieve-home
```

安装 Miniforge 到仓库内：

```bash
HOME=/tmp/pensieve-home \
bash /tmp/Miniforge3-pensieve.sh -b -p \
  /home/quic2/masque-HAS/cross-nossdav-go/.tools/miniforge3-pensieve-local
```

如果这个目录之前已经存在，可以使用更新模式重新执行：

```bash
HOME=/tmp/pensieve-home \
bash /tmp/Miniforge3-pensieve.sh -b -u -p \
  /home/quic2/masque-HAS/cross-nossdav-go/.tools/miniforge3-pensieve-local
```

## 5. 创建 Python 3.8 环境

```bash
HOME=/tmp/pensieve-home \
/home/quic2/masque-HAS/cross-nossdav-go/.tools/miniforge3-pensieve-local/bin/conda \
  create -y -p /home/quic2/masque-HAS/cross-nossdav-go/.tools/pensieve-py38 \
  python=3.8
```

检查 Python 版本：

```bash
/home/quic2/masque-HAS/cross-nossdav-go/.tools/pensieve-py38/bin/python --version
```

期望看到类似：

```text
Python 3.8.20
```

## 6. 安装 Pensieve 运行依赖

用刚创建好的 Python 安装依赖：

```bash
/home/quic2/masque-HAS/cross-nossdav-go/.tools/pensieve-py38/bin/python \
  -m pip install \
  tensorflow==2.7.0 \
  tflearn==0.5.0 \
  'protobuf<3.21' \
  'Pillow<10'
```

这些版本约束是必要的：

- `tensorflow==2.7.0`：匹配当前 Pensieve 迁移代码
- `tflearn==0.5.0`：Pensieve 网络结构依赖它
- `protobuf<3.21`：避免 TensorFlow 2.7 和新版 protobuf 的 descriptor 兼容问题
- `Pillow<10`：避免 TFLearn 访问已删除的 `Image.ANTIALIAS` 时报错

## 7. 检查依赖是否可导入

执行：

```bash
/home/quic2/masque-HAS/cross-nossdav-go/.tools/pensieve-py38/bin/python \
  -c "import sys; print(sys.executable); print(sys.version); import tensorflow as tf; print('tensorflow', tf.__version__); import tflearn; print('tflearn ok')"
```

期望看到：

```text
/home/quic2/masque-HAS/cross-nossdav-go/.tools/pensieve-py38/bin/python
3.8.20 ...
tensorflow 2.7.0
tflearn ok
```

可能还会看到：

```text
Scipy not supported!
Could not load dynamic library 'libcudart.so.11.0'
```

这些不影响 Pensieve 推理服务启动。

## 8. 启动 Pensieve 服务

必须从 `rl_server/` 目录启动，因为脚本内部使用相对路径加载模型文件：

```bash
cd /home/quic2/masque-HAS/cross-nossdav-go/paper-utilities/pensieve/rl_server
/home/quic2/masque-HAS/cross-nossdav-go/.tools/pensieve-py38/bin/python \
  rl_server_no_training.py
```

启动成功的关键输出是：

```text
Model restored.
Listening on port 8333
```

服务默认监听：

```text
http://127.0.0.1:8333
```

这个终端要保持打开。关闭终端或按 `Ctrl+C` 会停止 Pensieve 服务。

## 9. 验证服务是否可用

另开一个终端，执行：

```bash
curl -sS -X POST http://127.0.0.1:8333 \
  -H 'Content-Type: application/json' \
  -d '{
    "lastquality": 0,
    "buffer": 4.0,
    "RebufferTime": 0.0,
    "lastChunkStartTime": 0.0,
    "lastChunkFinishTime": 800.0,
    "lastChunkSize": 500000.0,
    "lastRequest": 1
  }'
```

如果返回一个数字，例如：

```text
1
```

说明：

- HTTP 服务已经监听成功
- checkpoint 已经恢复
- actor 网络已经完成一次推理
- GoDASH 后续可以通过 `-pensieveServer http://127.0.0.1:8333` 调用它

返回值不是固定的，可能是 `0` 到 `5` 之间的动作编号，也可能在最后一段返回 `REFRESH`。

## 10. GoDASH 调用方式

保持 Pensieve 服务运行，然后在 GoDASH 启动参数中加入：

```bash
-adapt pensieve -pensieveServer http://127.0.0.1:8333
```

示例形式：

```bash
cd /home/quic2/masque-HAS/cross-nossdav-go/cross-layer-implementation/godash-qlogabr
go run . -adapt pensieve -pensieveServer http://127.0.0.1:8333 ...
```

省略号部分需要替换为你本次实验原本使用的 MPD、输出目录和其他播放参数。

## 11. 常见问题

### 11.1 `ModuleNotFoundError: No module named 'tensorflow'`

说明你用的是系统 Python 或其他错误环境。

请确认启动命令使用的是：

```bash
/home/quic2/masque-HAS/cross-nossdav-go/.tools/pensieve-py38/bin/python
```

不要直接用 `python3 rl_server_no_training.py`。

### 11.2 找不到 checkpoint 或恢复模型失败

确认启动位置必须是：

```bash
/home/quic2/masque-HAS/cross-nossdav-go/paper-utilities/pensieve/rl_server
```

并确认模型文件存在：

```bash
ls results/pretrain_linear_reward.ckpt*
```

正常应能看到：

```text
results/pretrain_linear_reward.ckpt.data-00000-of-00001
results/pretrain_linear_reward.ckpt.index
results/pretrain_linear_reward.ckpt.meta
```

### 11.3 `PermissionError: [Errno 1] Operation not permitted`

如果在受限沙箱中运行，可能无法创建监听 socket。正常终端中直接执行启动命令即可；如果通过受限执行器运行，需要允许它监听本机端口。

### 11.4 端口已经被占用

检查是否已经有 Pensieve 服务在运行：

```bash
curl -sS -X POST http://127.0.0.1:8333 \
  -H 'Content-Type: application/json' \
  -d '{"lastquality":0,"buffer":4.0,"RebufferTime":0.0,"lastChunkStartTime":0.0,"lastChunkFinishTime":800.0,"lastChunkSize":500000.0,"lastRequest":1}'
```

如果能返回数字，说明服务已经可用，不需要重复启动。

### 11.5 想重新开始一次干净实验

官方 `rl_server_no_training.py` 没有单独的 `/reset` API。最稳妥的方法是：

1. 在 Pensieve 服务终端按 `Ctrl+C`
2. 重新执行第 8 节的启动命令
3. 再启动 GoDASH 实验

## 12. 当前已经确认的兼容性修正

当前子模块中已经包含这些 Python 3 / TensorFlow 2 兼容修正：

- `paper-utilities/pensieve/rl_server/a3c.py`
  - 使用 `tf.compat.v1.disable_eager_execution()`
  - 将旧 `reduction_indices` 参数改为 `axis`
  - 将 `tf.log` 改为 `tf.math.log`
- `paper-utilities/pensieve/rl_server/rl_server_no_training.py`
  - 使用 Python 3 的 `http.server`
  - HTTP 写回使用 bytes
  - 日志文件使用文本模式打开

## 13. 当前边界

- 当前服务仍要求从 `rl_server/` 目录启动
- 默认码率集合仍是 Pensieve 原始固定梯度：`300, 750, 1200, 1850, 2850, 4300`
- 默认使用脚本内部固定的视频 chunk size 表
- CPU 推理可行；训练会明显更慢
