# Pensieve 外部推理接入修改说明

本文档描述本次为 `godash-qlogabr` 接入官方 Pensieve 风格“外部 ABR 服务”所做的修改，并说明如何确认当前复现是否准确。

## 1. 本次修改的原则

本次修改遵循的系统边界是：

- `GoDASH` 负责：
  - chunk 下载
  - buffer / stall / QoE / 日志
  - 从 MPD 和运行时状态中提取 Pensieve 所需输入
  - 调用外部 Pensieve 服务拿到下一段动作
- `Pensieve service` 负责：
  - 维护 `S_INFO x S_LEN` 状态历史
  - 加载预训练 checkpoint
  - 运行 actor 网络推理
  - 返回下一段动作索引

因此，这次修改已经回退掉“在 Go 里内部重写 Pensieve 策略”的方向，改成了与官方 `hongzimao/pensieve` 中 `rl_server` / `real_exp` 一致的外部服务方式。

## 2. 代码修改清单

### 2.1 GoDASH 侧

- `cross-layer-implementation/godash-qlogabr/global/globalVar.go`
  - 新增 `PensieveAlg = "pensieve"`
  - 新增 `PensieveServerName = "pensieveServer"`

- `cross-layer-implementation/godash-qlogabr/logging/configParsing.go`
  - 配置结构新增 `pensieveServer`
  - `Configure(...)` / `recupParameters(...)` 返回值增加 `pensieveServer`

- `cross-layer-implementation/godash-qlogabr/main.go`
  - `algorithmSlice` 加入 `pensieve`
  - `-adapt` 帮助字符串加入 `pensieve`
  - 新增参数：
    - `-pensieveServer http://127.0.0.1:8333`
  - 调整 `player.Stream(...)` 调用，将 `pensieveServer` 传给播放器主流程

- `cross-layer-implementation/godash-qlogabr/player/player.go`
  - `pensieve` 首段仍从最低码率启动，与官方 Pensieve 常见启动方式一致
  - 在流开始时创建 `PensieveExternalClient`
  - 播放开始前调用 `POST /reset` 清空服务端状态
  - 下载每个 chunk 后，收集下列输入并调用 `POST /predict`
    - `lastquality`
    - `buffer`
    - `RebufferTime`
    - `lastChunkStartTime`
    - `lastChunkFinishTime`
    - `lastChunkSize`
    - `nextChunkSizes`
    - `video_chunk_remain`
    - `videoBitRate`
  - 新增一个强约束：
    - 当前 `AdaptationSet` 必须恰好有 6 个表示，否则直接退出
  - 修正尾段行为：
    - 如果已经没有“下一段”，则不再向 Pensieve 服务请求动作，直接保持当前表示索引

- `cross-layer-implementation/godash-qlogabr/algorithms/pensieve_external.go`
  - 新增 Go 侧 HTTP 客户端
  - 负责：
    - 调用 `/reset`
    - 维护累计 rebuffer 时间
    - 将 GoDASH 本地表示索引按带宽升序映射到 Pensieve `0..5` 动作空间
    - 从 MPD 的 `Representation[i].Chunks` 中取出“下一段各码率大小”
    - 调用 `/predict`
    - 将 Pensieve 返回的动作索引再映射回 GoDASH 本地表示索引

### 2.2 外部服务侧

- `paper-utilities/pensieve-abr-server/pensieve_service.py`
  - 新增独立 Python 推理服务
  - 使用 `tensorflow.compat.v1` + `tflearn`
  - 内部保留官方 Pensieve 的 actor 网络结构
  - 维护 `6 x 8` 状态矩阵
  - 暴露两个 HTTP 接口：
    - `POST /reset`
    - `POST /predict`
  - 通过 `--model` 加载预训练 checkpoint

- `paper-utilities/pensieve-abr-server/requirements.txt`
  - 补充推荐依赖版本：
    - `tensorflow==1.15.5`
    - `tflearn==0.5.0`
    - `numpy<2`

## 3. 当前实现与官方 Pensieve 的对应关系

当前实现已经与官方架构对齐在这些方面：

- 决策服务在播放器外部
- 播放器每个 chunk 完成后上传状态
- 服务端维护历史状态窗口
- 服务端加载 checkpoint 并输出动作
- 动作空间固定为 `A_DIM = 6`

当前实现仍然有两个需要你们自行确认的外部条件：

1. 你们使用的 checkpoint 是否确实来自官方 `hongzimao/pensieve` 或明确来源的预训练模型
2. 你们的 MPD 六档码率梯度是否适合直接套用该 checkpoint

也就是说：

- 当前代码已经是“正确的系统边界复现”
- 但是否达到“严格意义上的官方模型效果复现”，还取决于模型文件和视频码率梯度是否匹配

## 4. 运行方式

### 4.1 启动 Pensieve 外部服务

在一个单独环境中准备官方 Pensieve 可兼容的 Python 运行时，推荐：

- Python 3.7
- TensorFlow 1.15.x
- TFLearn 0.5.0

然后启动服务：

```bash
cd paper-utilities/pensieve-abr-server
pip install -r requirements.txt
python pensieve_service.py --model /path/to/pretrain_linear_reward.ckpt --port 8333
```

如果你想减少随机性，方便和日志对照，可以加：

```bash
python pensieve_service.py --model /path/to/pretrain_linear_reward.ckpt --port 8333 --deterministic
```

### 4.2 启动 GoDASH

```bash
cd cross-layer-implementation/godash-qlogabr
go run . -adapt pensieve -pensieveServer http://127.0.0.1:8333 ...
```

也可以把 `pensieveServer` 写入配置文件，通过 `logging/configParsing.go` 新增的字段注入。

## 5. 如何确认“复现状态正确”

建议从四层检查。

### 5.1 架构层检查

确认以下事实：

- `player.go` 没有再实现内部 Pensieve 打分器
- GoDASH 只是在下载后向 `/predict` 发请求
- Python 服务才持有 actor 网络和状态窗口

只要这三点成立，架构边界就已经回到官方设计。

### 5.2 接口层检查

先确认服务已启动，然后手工测试：

```bash
curl -X POST http://127.0.0.1:8333/reset
```

再发一个最小预测请求：

```bash
curl -X POST http://127.0.0.1:8333/predict \
  -H 'Content-Type: application/json' \
  -d '{
    "lastquality": 0,
    "buffer": 4.0,
    "RebufferTime": 0,
    "lastChunkStartTime": 0,
    "lastChunkFinishTime": 800,
    "lastChunkSize": 500000,
    "nextChunkSizes": [400000,600000,900000,1200000,1600000,2200000],
    "video_chunk_remain": 40,
    "videoBitRate": [300,750,1200,1850,2850,4300]
  }'
```

如果返回 `0..5` 的整数，说明服务接口正常。

### 5.3 运行层检查

让 GoDASH 以 `-adapt pensieve` 跑一次，确认：

- 启动时没有出现
  - `failed to reset Pensieve service`
  - `pensieve service request failed`
- 码率日志中确实发生切换
- 网络状态变化时，动作不是常量

如果全程只输出同一个动作，需要重点检查：

- checkpoint 是否加载成功
- 你是否启用了 `--deterministic`
- `nextChunkSizes` / `videoBitRate` 是否始终正确变化

### 5.4 准确性层检查

若你们想判断“这是否是准确复现官方 Pensieve”，最低建议做这几项：

1. 确认使用的是官方或可追溯来源的 checkpoint
2. 确认输入归一化方式与官方一致
3. 确认动作空间仍是 6 档
4. 在一条固定 trace 上记录每个 chunk 的请求 payload 和返回动作
5. 与官方 `real_exp` / `rl_server` 在同类输入上的动作序列做对照

如果同一组输入下动作序列一致，才能进一步说明“模型推理级别也对齐了”。

## 6. 当前已知限制

- `pensieve` 模式要求当前 `AdaptationSet` 恰好有 6 个表示
- Python 服务依赖 TensorFlow 1.x 生态，环境要求高于 GoDASH 本体
- 当前仓库里的老版本 `quic-go` 与本机 `go1.22.8` 不兼容，所以无法在当前环境完成完整 `go test` 构建验证

最后这一点属于仓库原有依赖限制，不是 Pensieve 外部服务接入本身的接口问题。

## 7. 这次修改后，什么才算“已经复现到位”

如果满足以下条件，可以认为已经完成了“架构上正确、实现边界正确”的 Pensieve 复现：

1. GoDASH 只负责状态采集与请求转发
2. 外部服务加载预训练 checkpoint
3. 服务按官方状态维度维护历史窗口
4. GoDASH 每段都基于服务返回动作选下一段码率

如果再额外满足：

1. 使用官方 checkpoint
2. 六档码率梯度与官方训练条件足够接近
3. 与官方服务在同输入下输出一致

那么才能进一步说“推理行为也较准确地复现了官方 Pensieve”。
