# Pensieve Py3.8 本机最小运行说明

本文档记录当前主机上实际跑通 `paper-utilities/pensieve/rl_server/rl_server_no_training.py` 的最小隔离环境方案。

## 1. 目标

目标是：

- 不污染主机系统 Python
- 在用户目录下创建独立 Python 3.8 环境
- 启动 Pensieve `rl_server_no_training.py`
- 用本地 HTTP 请求验证服务可以返回码率动作

当前主机不具备 GPU，这不影响在线推理。

## 2. 为什么没有直接用系统 `venv`

这台主机的实际情况是：

- 系统只有 `python3.10`
- 没有 `python3.8`
- `python3 -m venv` 依赖的 `ensurepip` 也不可用

因此这里采用用户目录下的 `Miniforge + conda env` 方案，仍然满足“隔离环境、不影响主机现有环境”的要求。

## 3. 最小安装命令

以下命令是在当前主机上实际执行并跑通的最小集合。

### 3.1 安装 Miniforge 到用户目录

```bash
mkdir -p ~/.local
cd ~/.local
curl -L -o Miniforge3.sh \
  https://github.com/conda-forge/miniforge/releases/latest/download/Miniforge3-Linux-x86_64.sh
bash Miniforge3.sh -b -p ~/.local/miniforge3-pensieve
```

### 3.2 创建独立 Python 3.8 环境

```bash
~/.local/miniforge3-pensieve/bin/conda create -y -n pensieve-py38 python=3.8
```

### 3.3 安装运行 `rl_server` 的最小依赖

```bash
~/.local/miniforge3-pensieve/bin/conda run -n pensieve-py38 \
  python -m pip install --upgrade pip

~/.local/miniforge3-pensieve/bin/conda run -n pensieve-py38 \
  python -m pip install \
  tensorflow==2.7.0 \
  tflearn==0.5.0 \
  'protobuf<3.21' \
  'Pillow<10'
```

这几个版本约束不是随意加的，而是这次实机启动时确认必须存在：

- `tensorflow==2.7.0`
- `tflearn==0.5.0`
- `protobuf<3.21`
  原因：避免 TensorFlow 2.7 与较新 `protobuf` 的 descriptor 兼容错误
- `Pillow<10`
  原因：避免 `tflearn` 仍访问 `Image.ANTIALIAS` 导致导入失败

## 4. 启动方式

必须从 `rl_server/` 目录启动，因为脚本内部仍按相对路径加载 checkpoint：

```bash
cd /home/quic/cross-that-boundary-mmsys23-nossdav/paper-utilities/pensieve/rl_server
~/.local/miniforge3-pensieve/bin/conda run -n pensieve-py38 \
  python rl_server_no_training.py
```

成功启动后的关键标志是输出：

```text
Model restored.
```

然后服务会监听：

```text
127.0.0.1:8333
```

## 5. 本地接口验证

在不涉及播放器的情况下，可以直接发送一个最小 POST 请求：

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

本次实机验证返回：

```text
1
```

这说明：

- 服务已成功监听本地端口
- checkpoint 已成功恢复
- actor 网络已完成一次推理
- HTTP 输入输出接口已可被 GoDASH 后续接入使用

## 6. 当前已经确认的兼容性修正

为了让这版 py38 子模块在当前主机上真正跑起来，本次已经确认需要这些代码兼容修正：

- `paper-utilities/pensieve/rl_server/a3c.py`
  - 启用 `tf.compat.v1.disable_eager_execution()`
  - 将旧 `reduction_indices` 参数改为 `axis`
  - 将 `tf.log` 改为 `tf.math.log`
- `paper-utilities/pensieve/rl_server/rl_server_no_training.py`
  - Python 3 的 `http.server` / `bytes` 写回兼容
  - 文本日志文件打开方式兼容

## 7. 当前边界

虽然 `rl_server` 已经在当前主机上跑通，但还要注意这些边界：

- 当前服务仍要求从 `rl_server/` 目录启动
- 仍使用 Pensieve 原始固定码率梯度：
  - `300, 750, 1200, 1850, 2850, 4300`
- 仍依赖脚本内部固定的视频 chunk size 表
- CPU 推理可行，但训练在无 GPU 主机上会明显更慢

## 8. 建议的后续验证

如果要进一步确认“和 GoDASH 的接线已经完整可用”，建议按下面顺序继续验证：

1. 先保持这个 `rl_server` 本地运行
2. 再让 GoDASH 用 `-adapt pensieve -pensieveServer http://127.0.0.1:8333`
3. 检查播放器侧每段请求后是否都能收到动作
4. 检查最后一段结束时是否正确处理 `REFRESH`
5. 再对照日志确认动作序列是否合理变化
