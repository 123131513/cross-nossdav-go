# Pensieve 在 GoDASH 中的正确复现方案

本文档给出一个与官方 `hongzimao/pensieve` 架构一致的复现方案。

核心结论只有一句：

- **GoDASH 不应该在内部重写 Pensieve 策略逻辑**
- **GoDASH 应该负责下载、缓冲、日志和状态采集**
- **Pensieve 应该作为外部 ABR 推理服务返回“下一段码率索引”**

这与官方仓库的设计是吻合的：

- `sim/`
  训练和模拟
- `test/`
  离线测试
- `rl_server/`
  外部推理服务
- `real_exp/`
  真实播放器向 ABR server 请求下一段码率

## 1. 为什么之前那种“Go 内部重写 Pensieve 打分器”不对

Pensieve 的本质不是一种普通规则型 ABR 算法。

它的核心是：

1. 固定的状态表示
2. A3C 训练出来的 actor 网络
3. 由 actor 网络输出动作分布
4. 运行时播放器只负责把观测状态发给外部服务

所以如果在 Go 里直接用规则或启发式“近似 Pensieve”，即便状态和奖励写得像，也不能说是在复现官方 Pensieve。

能称为正确复现的最小条件是：

1. 使用官方 Pensieve 风格的外部决策服务
2. 使用预训练 checkpoint 做在线推理
3. GoDASH 只做状态采集和请求转发，不篡改策略本体

## 2. 与官方 `hongzimao/pensieve` 对齐的运行边界

本项目现在采用的边界是：

### GoDASH 负责

- 下载 chunk
- 维护 buffer
- 记录 stall / QoE / 日志
- 采集每个 chunk 下载后的运行时状态
- 调用外部 Pensieve 服务
- 将服务返回的动作索引映射回本地表示索引

### Pensieve 外部服务负责

- 维护 `S_INFO x S_LEN` 状态历史
- 加载预训练 actor checkpoint
- 执行神经网络推理
- 返回下一段码率动作

这才是与官方 `rl_server/rl_server_no_training.py` 设计一致的形式。

## 3. 官方 Pensieve 服务到底需要什么输入

根据官方 `rl_server/rl_server_no_training.py`，在线推理本质上需要这些 chunk 级观测：

1. `lastquality`
   上一段选择的质量档位索引
2. `buffer`
   当前 buffer 占用
3. `RebufferTime`
   到当前为止累计 rebuffer 时间
4. `lastChunkStartTime`
5. `lastChunkFinishTime`
   这两个量之差是上一段下载时长
6. `lastChunkSize`
   上一段 chunk 大小
7. `nextChunkSizes`
   下一段在各动作档位下的 chunk 大小
8. `video_chunk_remain`
   剩余 chunk 数

官方服务据此维护状态矩阵：

- `state[0]` 上一段码率
- `state[1]` buffer
- `state[2]` 吞吐观测
- `state[3]` 下载时延
- `state[4]` 下一段各动作大小
- `state[5]` 剩余 chunk 数

所以正确做法不是让 GoDASH 把 Pensieve 的完整状态机塞回本地，而是：

- GoDASH 提供这些原始输入
- 由外部 Pensieve server 完成状态滚动和模型推理

## 4. 当前仓库里这些状态从哪里来

在 `godash-qlogabr/player/player.go` 中，GoDASH 已经具备全部必要输入：

- 当前 buffer：`bufferLevel`
- 本次 stall：`stallTime`
- 当前 chunk 大小：`segSize`
- 当前 chunk 下载时延：`deliveryTime`
- 当前码率索引：`preRepRate`
- 下一段各表示 chunk size：
  - `Representation[i].Chunks`
  - `utils.GetChunk(..., segmentNumber+1)`

因此 GoDASH 本身不缺数据，缺的是：

- 一个正确的外部服务调用边界
- 一个兼容官方 checkpoint 的推理服务

## 5. 正确复现的工程方案

## 5.1 Go 侧

在 GoDASH 中新增 `pensieve` 算法，但它不包含内部策略，只包含：

1. 初始化外部客户端
2. 每个 chunk 下载后发起一次 HTTP 请求
3. 获取服务返回的动作索引
4. 将动作索引映射为本地表示索引

## 5.2 Python 侧

新增一个独立 Python 服务，作用是：

1. 维护 Pensieve 状态窗口
2. 加载预训练模型 checkpoint
3. 运行 actor 网络推理
4. 返回动作索引

## 5.3 与官方仓库的差异控制

为了尽量贴近官方仓库：

- actor 网络结构沿用官方 `a3c.py` 的 actor 网络
- 输入状态维度保持 `S_INFO=6`, `S_LEN=8`, `A_DIM=6`
- 仍然采用 actor 输出分布，再通过随机采样或 argmax 选动作

唯一做的工程性适配是：

- 官方 `rl_server_no_training.py` 默认把下一段 chunk size 写死在脚本里
- 我们改成由 GoDASH 直接把 `nextChunkSizes` 传给服务

这样做更适合 GoDASH，因为 GoDASH 已经能从 MPD 中拿到真实下一段大小。

## 6. 这次复现的准确性边界

当前方案中，只有在以下条件满足时，才能说“比较准确地复现了官方 Pensieve 在线推理”：

1. 使用外部服务，而不是 Go 内部启发式替代
2. 服务加载的是官方预训练 checkpoint
3. 状态输入维度和归一化方式与官方一致
4. 动作空间与官方一致：`A_DIM = 6`
5. 视频表示数也为 6

如果 MPD 不是 6 档表示，则：

- 无法直接对应官方 6 动作 actor
- 就不能说是准确复现了官方预训练模型

如果 MPD 是 6 档，但码率梯度和官方训练视频不完全一致，则：

- 可以运行
- 但只能称为“官方模型在新码率梯度上的迁移使用”
- 不能称为严格数值复现

## 7. 当前实现的策略

当前仓库将采用一个严格限制：

- `pensieve` 模式要求 GoDASH 当前 AdaptationSet 恰好有 6 个表示

这样至少保证：

- 动作空间与官方 checkpoint 一致

至于码率值是否与官方训练视频完全一致，需要用户额外确认。

## 8. 如何确认“我们复现得准确”

需要分三层确认。

### 第一层：架构是否正确

确认以下点：

1. GoDASH 不再在本地实现 Pensieve 打分器
2. GoDASH 只是在每个 chunk 后向外部服务发请求
3. 外部服务加载 checkpoint 并返回动作

如果这三点满足，说明架构已经对齐官方设计。

### 第二层：接口是否正确

确认以下点：

1. GoDASH 发给服务的字段包含：
   - `lastquality`
   - `buffer`
   - `RebufferTime`
   - `lastChunkStartTime`
   - `lastChunkFinishTime`
   - `lastChunkSize`
   - `nextChunkSizes`
   - `video_chunk_remain`
   - `videoBitRate`
2. 服务内部状态维度是 `6 x 8`
3. 服务返回的是 `0..5` 之间的动作索引

### 第三层：模型是否正确

确认以下点：

1. 载入的是官方或明确来源的预训练 checkpoint
2. Python 服务打印了模型恢复成功
3. 服务输出动作分布不是恒定值
4. GoDASH 日志中可以看到 `pensieve` 在不同网络状态下确实切换码率

如果还要进一步证明“接近官方结果”，则需要：

1. 使用与官方接近的 6 档视频梯度
2. 使用官方推荐的 checkpoint，例如线性 QoE reward 的 checkpoint
3. 在同类 trace 上与官方脚本结果做对照

## 9. 我们最终要交付什么

正确的交付物应该包括：

1. GoDASH 侧的 Pensieve 外部调用逻辑
2. 一个可以加载 checkpoint 的 Python 推理服务
3. 启动和联调文档
4. 一份“如何判断复现准确”的说明

而不是：

- 在 Go 里写一个规则型“Pensieve-like”算法

## 10. 结论

正确复现 Pensieve 的第一步不是“把策略抄成 Go 规则”，而是恢复官方的系统边界：

- GoDASH = player / state collector / downloader
- Pensieve server = external inference service

本次修改将严格按这个边界推进，并把“准确复现的判据”写进对应文档。
