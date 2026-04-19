# Pensieve 子模块接入与播放器运行说明

本文档说明本次如何把 `Pensieve` 作为官方代码子模块接入当前仓库、当前实际可运行边界是什么，以及满足条件后如何在 `GoDASH` 播放器中调用它。

## 1. 这次调整的原则

本次不再维护自写的 `pensieve_service.py` 复制实现。

改为：

- 直接把你们 fork 的 `Pensieve` 仓库接入为子模块
- 运行时直接使用子模块中的 `rl_server/rl_server_no_training.py`
- 当前仓库只保留播放器侧的接线逻辑，用来把 `GoDASH` 的 chunk 级运行时状态发给官方 Pensieve 服务

这样做的目的很明确：

- 避免在本仓库中重复维护一份 Pensieve 服务代码
- 尽量保持与官方 `hongzimao/pensieve` 的目录边界和 `rl_server` 逻辑一致
- 后续如果你们在 Pensieve fork 中修正模型路径、环境依赖或实验脚本，当前仓库只需要更新子模块指针

需要先说明当前状态：

- Go 侧兼容层已经接入仓库
- 子模块中的预训练 checkpoint 文件已经存在
- 子模块代码已经替换为 Python 3.8 / TensorFlow 2.7 迁移版
- 但当前机器仍然没有安装 `tensorflow` / `tflearn`
- 因此 `rl_server_no_training.py` 目前在这台机器上仍然不能直接启动

## 2. 新增的子模块

当前仓库已新增子模块：

- 路径：`paper-utilities/pensieve`
- 远程：`https://github.com/123131513/pensieve.git`

对应的 git 配置在：

- `.gitmodules`

克隆当前仓库后，使用者需要额外执行：

```bash
git submodule update --init --recursive
```

否则 `paper-utilities/pensieve` 目录只有子模块占位，不会有实际代码。

## 3. 当前仓库中保留了哪些 Pensieve 相关修改

### 3.1 GoDASH 侧入口

- `cross-layer-implementation/godash-qlogabr/global/globalVar.go`
  - 新增 `PensieveAlg = "pensieve"`
  - 新增 `PensieveServerName = "pensieveServer"`

- `cross-layer-implementation/godash-qlogabr/logging/configParsing.go`
  - 配置结构新增 `pensieveServer`

- `cross-layer-implementation/godash-qlogabr/main.go`
  - `-adapt` 支持 `pensieve`
  - 新增 `-pensieveServer`

### 3.2 GoDASH 播放器侧兼容层

- `cross-layer-implementation/godash-qlogabr/player/player.go`
  - `pensieve` 首段从最低码率启动
  - 每个 chunk 下载完成后，调用 Pensieve 外部服务
  - 服务返回动作索引后，再映射回本地表示索引

- `cross-layer-implementation/godash-qlogabr/algorithms/pensieve_external.go`
  - 负责与外部 Pensieve 服务通信
  - 负责把 GoDASH 本地表示索引按码率升序映射到 `0..5`
  - 负责把 Pensieve 返回的动作再映射回本地表示索引

## 4. 这次删除了什么

已删除本仓库里自写的 Pensieve 服务副本：

- `paper-utilities/pensieve-abr-server/pensieve_service.py`
- `paper-utilities/pensieve-abr-server/requirements.txt`

原因是这部分逻辑现在应由子模块 `paper-utilities/pensieve` 中的官方代码承担。

## 5. 与官方 `rl_server` 的兼容方式

当前播放器侧兼容层按 `rl_server/rl_server_no_training.py` 的请求字段发送：

- `lastquality`
- `buffer`
- `RebufferTime`
- `lastChunkStartTime`
- `lastChunkFinishTime`
- `lastChunkSize`
- `lastRequest`

这几点和官方服务是对齐的。

另外需要明确一个重要约束：

- 官方 `rl_server_no_training.py` 内部写死了六档码率：
  - `300, 750, 1200, 1850, 2850, 4300` Kbps

因此当前 GoDASH 的 `pensieve` 模式做了严格限制：

1. 当前 `AdaptationSet` 必须恰好有 6 个表示
2. 六档码率必须与官方码率梯度一致

如果不满足这两个条件，当前实现不会把它当作“官方 Pensieve 一致复现”来运行。

## 6. 如何运行 Pensieve 到当前播放器中

### 6.1 初始化子模块

```bash
git submodule update --init --recursive
```

### 6.2 准备 Pensieve 环境

进入子模块：

```bash
cd paper-utilities/pensieve
```

按照当前子模块 README，这一版迁移代码的目标环境是：

- Ubuntu 18.04
- Python 3.8
- TensorFlow 2.7.0
- TFLearn 0.5.0

当前这版 `rl_server/rl_server_no_training.py` 已经改成 Python 3 语法，并使用 `tf.compat.v1` 路径；当前仓库另外补了几处最小修正，使它在 Python 3 下的 I/O 行为与 HTTP 响应格式保持一致。

但需要注意：

- 子模块中的 `setup.py` 仍然保留了大量旧版系统安装逻辑
- 它会尝试安装 `mahimahi`、`apache2`、`selenium 2.39.0` 和系统级 Python 包
- 在现代主机上不建议直接执行 `python setup.py`

更稳妥的方式是：

- 手动创建 Python 3.8 虚拟环境
- 手动安装 `tensorflow`、`tflearn`、`matplotlib`、`selenium`
- 只单独运行 `rl_server_no_training.py`

如果你们使用自己的 fork，建议至少先在 `paper-utilities/pensieve` 中确认：

1. `rl_server/rl_server_no_training.py` 的 `NN_MODEL` 指向你们要用的 checkpoint
2. 运行环境确实能启动该脚本

### 6.3 启动官方 Pensieve rl_server

```bash
cd paper-utilities/pensieve/rl_server
python3 rl_server_no_training.py
```

默认监听端口是 `8333`。

这一步的真实前提是：

- 环境中必须安装 TensorFlow 2.7.x / TFLearn 0.5.0 或兼容版本
- 当前 Python 环境必须能正确导入 `tensorflow` 和 `tflearn`

如果像当前机器这样还没有安装 `tensorflow`，直接执行会报导入错误，而不是协议错误。

注意：

- 官方 `rl_server_no_training.py` 没有单独的 `/reset` API
- 如果你们想确保每次实验都从干净状态开始，最稳妥的方法是：
  - 每次播放器实验前重启一次这个 Python 进程
- 官方服务在最后一个 chunk 会返回 `REFRESH`
- 当前播放器侧已经对这个尾段返回值做了兼容处理

### 6.4 启动当前播放器

```bash
cd cross-layer-implementation/godash-qlogabr
go run . -adapt pensieve -pensieveServer http://127.0.0.1:8333 ...
```

其中：

- `-adapt pensieve` 启用 Pensieve
- `-pensieveServer` 指向官方 `rl_server` 服务地址

## 7. 如何确认现在确实在“用官方 Pensieve”

建议按下面顺序确认。

### 7.1 代码来源确认

确认这两个事实：

1. 目录 `paper-utilities/pensieve` 是 git submodule
2. 实验时启动的是：
   - `paper-utilities/pensieve/rl_server/rl_server_no_training.py`

如果这两点成立，就说明你们运行的是子模块中的官方服务代码，而不是当前仓库的复制实现。

### 7.2 码率梯度确认

确认 MPD 的 6 档码率升序后正好是：

```text
300, 750, 1200, 1850, 2850, 4300 Kbps
```

如果不是这组值，那么即使服务跑起来，也不能说是“严格沿用官方 rl_server 的一致复现”，因为官方服务内部 reward 和 chunk-size 表都绑定了这组码率与视频。

### 7.3 模型确认

确认 `rl_server_no_training.py` 中的 `NN_MODEL` 指向你们想使用的 checkpoint，并且启动时能看到模型恢复成功。

### 7.4 运行确认

在播放器日志里确认：

- 没有 `pensieve service request failed`
- 码率动作会变化
- 网络条件变化时，动作不是常量

## 8. 当前准确性边界

当前方案相较于自写服务代码，更接近“直接使用官方 Pensieve 方案”。

但准确性仍然取决于两个外部前提：

1. 你们实际启动的确实是子模块中的官方 `rl_server_no_training.py`
2. 你们的视频内容、chunk 大小和六码率梯度与官方服务内置假设一致

只有在这些条件成立时，才能较强地声称：

- 当前播放器是在调用官方 Pensieve 推理服务

如果视频内容或 chunk size 已经不同，只能说：

- 当前播放器在复用官方 Pensieve 服务代码路径
- 但实验条件未必与论文原始服务假设完全等价

如果环境本身还不满足 Python 3.8 / TensorFlow / TFLearn 依赖，则当前状态只能进一步表述为：

- 仓库结构和调用边界已经对齐官方 Pensieve
- 但服务端还没有在当前机器上成功运行

另外还要区分推理和训练：

- 在线推理不需要 GPU，CPU 即可运行
- 重新训练在无 GPU 主机上可以做，但速度会明显较慢

## 9. 后续维护方式

之后如果你们要更新 Pensieve 侧实现，建议只在子模块仓库中改：

- `123131513/pensieve`

然后回到当前仓库更新子模块指针并提交。

这样能保持职责分离：

- 当前仓库维护播放器与接线
- 子模块仓库维护 Pensieve 原始方案代码
