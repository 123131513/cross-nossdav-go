# ABR 算法与跨层通信实现说明

本文档专门面向两个问题：

1. 论文中的 ABR 算法和跨层通信在 `godash-qlogabr/` 与 `quic-go/` 中是如何实现的。
2. 运行时如何切换不同算法，以及如果需要新增一个 ABR 算法，应该修改哪些文件。

## 1. 先给结论

这个仓库里的“论文方法”不是只在一个文件里完成的，而是一个分层链路：

1. `godash-qlogabr/main.go`
   解析 `-adapt`，初始化跨层会计器 `CrossLayerAccountant`，把它注入 HTTP/QUIC 下载层。
2. `godash-qlogabr/http/urlParsing.go`
   创建 `http3.RoundTripper` 时，把 `quic-go/qlog.NewTracer(...)` 接到 QUIC 配置里，并把 qlog 事件写入 `CrossLayerAccountant.EventChannel`。
3. `quic-go/qlog/qlog.go`
   在收到 QUIC 包、更新 RTT、丢包等时记录 qlog 事件；同时把这些事件送到跨层事件通道。
4. `godash-qlogabr/crosslayer/crosslayerHelpers.go`
   监听 qlog 事件，把包到达长度累积成跨层吞吐估计，并在需要时执行 stall predictor / abort。
5. `godash-qlogabr/player/player.go`
   在每个分片开始下载前决定是否启用跨层预测；在分片下载完成后执行具体 ABR 算法，决定下一段表示。
6. `godash-qlogabr/algorithms/*.go`
   放各个 ABR 的具体决策公式；论文的核心 BBA2 和跨层变体主要在这里和 `crosslayer/` 一起完成。

一句话概括：

- `godash-qlogabr` 负责“做 ABR 决策”
- `quic-go` 负责“提供传输层细粒度事件”
- `crosslayerHelpers.go` 负责“把 QUIC 事件转成可被 ABR 使用的跨层信号”

---

## 2. `godash-qlogabr/`：ABR 与跨层逻辑主实现

## 2.1 算法名和算法白名单在哪里定义

### 关键文件

- `cross-layer-implementation/godash-qlogabr/global/globalVar.go`
- `cross-layer-implementation/godash-qlogabr/main.go`

### 作用

`global/globalVar.go` 定义所有算法字符串常量：

- `bba2`
- `bba2XL-base`
- `bba2XL-rate`
- `bba2XL-double`
- `averageXL`
- `averageRecentXL`
- `pensieve`

这些名字是运行时切换算法的真实标识。

`main.go` 里有两个直接影响算法切换的地方：

1. `algorithmSlice`
   算法白名单，决定 `-adapt` 传入的名字是否合法。
2. `adaptPtr := flag.String(...)`
   命令行 `-adapt` 参数帮助信息。

### 当前代码里的一个不一致点

`globalVar.go` 定义了：

- `const BBA2Alg_AVXL_rate = "bba2XL-rate"`

并且 `player.go` 里也有 `case glob.BBA2Alg_AVXL_rate`

但是 `main.go` 的 `algorithmSlice` 目前没有把 `glob.BBA2Alg_AVXL_rate` 加进去，`-adapt` 帮助字符串也没有列出它。这意味着：

- 代码实现了 `bba2XL-rate`
- 但用户无法通过正常参数校验来选中它

所以如果你们后续要真的用这个算法，首先要修 `main.go`。

补充说明：

- `pensieve` 已经被加入 `main.go` 的算法白名单
- 它不属于论文原始 BBA2 / Cross-layer 主线，而是后续新增的“外部 ABR 服务”模式
- 它依赖 `paper-utilities/pensieve` 子模块中的官方 `rl_server/rl_server_no_training.py`

---

## 2.2 程序启动时，跨层对象如何接入

### 关键文件

- `cross-layer-implementation/godash-qlogabr/main.go`
- `cross-layer-implementation/godash-qlogabr/http/urlParsing.go`

### 调用链

`main.go` 在启动早期做了这几件事：

1. 创建 qlog 事件通道：
   `qlogEventChan := make(chan qlog.Event)`
2. 创建跨层对象：
   `accountant := &xlayer.CrossLayerAccountant{EventChannel: qlogEventChan}`
3. 启动监听：
   `accountant.Listen(true)`
4. 注入 HTTP/下载模块：
   `http.SetAccountant(accountant)`

也就是说，`CrossLayerAccountant` 是上层 GoDASH 的跨层核心对象；它持有一个通道，后续 QUIC qlog 事件会被写进来。

这里要区分两类算法：

- 论文主线跨层算法
  直接依赖 `CrossLayerAccountant` 的吞吐统计、stall predictor 和 abort 逻辑
- `pensieve`
  仍然走 GoDASH 主下载流程，但策略决策通过 HTTP 请求交给外部 Pensieve 服务，当前并不使用 `CrossLayerAccountant` 的预测器输出

---

## 2.3 QUIC 事件是怎么被接到 GoDASH 里的

### 关键文件

- `cross-layer-implementation/godash-qlogabr/http/urlParsing.go`
- `cross-layer-implementation/quic-go/qlog/qlog.go`

### 核心机制

在 `http/urlParsing.go` 的 `GetHTTPClient(quicBool, ...)` 中，如果启用 QUIC：

1. 创建 `quic.Config{}`
2. 设置 `qconf.Tracer = qlog.NewTracer(...)`
3. 把 `globAccountant.EventChannel` 传给 `quic-go/qlog.NewTracer(...)`

这一步非常关键，因为它把：

- QUIC 层的 tracer
- GoDASH 的 `CrossLayerAccountant.EventChannel`

连接到了一起。

然后在 `quic-go/qlog/qlog.go` 中：

- `NewTracer(...)` 保存 `events_crossLayer chan Event`
- `TracerForConnection(...)` 创建每个连接对应的 `connectionTracer`
- `connectionTracer.recordEvent(...)` 会把事件：
  - 写入标准 qlog 文件
  - 同时写入 `events_crossLayer`

也就是说，论文中的“跨层信息共享”不是通过 socket API 直接暴露 RTT，也不是 GoDASH 主动调用 QUIC 内部结构，而是：

`quic-go qlog event -> EventChannel -> CrossLayerAccountant`

这是一个很干净的解耦设计。

---

## 2.4 `CrossLayerAccountant` 到底做了什么

### 关键文件

- `cross-layer-implementation/godash-qlogabr/crosslayer/crosslayerHelpers.go`

### 核心职责

`CrossLayerAccountant` 有三类职责：

1. 监听 QUIC qlog 事件，估算跨层吞吐
2. 记录分片下载计时
3. 在跨层版本算法里执行 stall prediction 与 abort

### 2.4.1 监听 QUIC 包事件

`channelListenerThread()` 会循环读取 `EventChannel`。

当前实现主要关注：

- `EventPacketReceived`

一旦收到这个事件，就取：

- `packetReceivedPointer.Length`

把每个收到的 QUIC 包长度追加到 `throughputList`。

这意味着这里估算的“跨层吞吐”不是按分片完成时间算的应用层吞吐，而是：

- 直接按 QUIC 包到达字节数
- 配合计时器 `StartTiming/StopTiming`
- 算出更细粒度的传输层平均速率

### 2.4.2 提供两种跨层吞吐接口

`crosslayerHelpers.go` 提供：

- `GetAverageThroughput()`
  返回整个测量窗口上的平均吞吐
- `GetRecentAverageThroughput()`
  返回最近 3000 个包上的平均吞吐

这两个接口分别被：

- `averageXL.go`
- `averageRecentXL.go`

调用。

### 2.4.3 执行 stall predictor 和 abort

对于 BBA1XL / BBA2XL 这类算法，`CrossLayerAccountant` 还会在分片开始时被配置为“预测模式”：

- `InitialisePredictor(...)`
- `SegmentStart_predictStall(...)`

`SegmentStart_predictStall(...)` 会记录：

- 当前段时长
- 当前表示码率
- 下载开始时缓冲区
- 当前段 chunksize
- 下一段低一档表示的 chunksize
- lower reservoir
- 一个 `cancel context.CancelFunc`

之后每收到一个 QUIC 包，`stallPredictor()` 就会更新：

- 当前窗口吞吐
- 当前段剩余比特数
- 以当前速率下载完所需时间
- 若降一档，下一段是否来得及

如果预测会 stall，就：

1. 记录 `STALLPREDICTOR` 指标
2. 把 `*aborted = true`
3. 调用 `cancel()`

这就是论文里“跨层提前中断下载”的实现核心。

---

## 2.5 `player.go` 是 ABR 真正的调度中心

### 关键文件

- `cross-layer-implementation/godash-qlogabr/player/player.go`

这个文件是最重要的单文件入口，因为它同时负责：

- 初始化 BBA2 状态
- 在每段开始前决定是否开启预测
- 调用 `http.GetFile(...)` 下载
- 在下载完成后调用具体算法函数决定下一段表示

另外，当前 `player.go` 还承担了 `pensieve` 接线工作：

- 在 `adapt == pensieve` 时创建 `PensieveExternalClient`
- 在每个分片下载完成后调用外部 `rl_server`
- 将服务返回的动作索引映射回本地表示索引

### 2.5.1 初始化预测器

在 `Stream(...)` 中：

- `bba1XL`、`bba2XL-base` 用 `crosslayer.Base`
- `bba2XL-rate` 用 `crosslayer.Rate`
- `bba2XL-double` 用 `crosslayer.Double`

对应代码是：

- `accountant.InitialisePredictor(&metricsLogger, crosslayer.Base/Rate/Double)`

这说明：

- `Base`
  基础跨层 abort 逻辑
- `Rate`
  带速率适配含义的变体
- `Double`
  会额外预测“如果当前段勉强完成，下一段低一档还能否及时到达”

注意：当前 `stallPredictor()` 里明显使用了 `Double` 的额外判断；`Rate` 在这里没有像 `Double` 一样体现出单独分支，因此它更像一个预留或未完全展开的变体。

### 2.5.2 Pensieve 分支是怎么接进去的

### 关键文件

- `cross-layer-implementation/godash-qlogabr/player/player.go`
- `cross-layer-implementation/godash-qlogabr/algorithms/pensieve_external.go`
- `paper-utilities/pensieve/rl_server/rl_server_no_training.py`

### 调用链

当前 `pensieve` 模式的链路是：

1. `main.go`
   解析 `-adapt pensieve` 和 `-pensieveServer`
2. `player.Stream(...)`
   在流开始时创建 `PensieveExternalClient`
3. `streamLoop(...)`
   每个分片完成后调用 `PensieveClient.SelectBitrate(...)`
4. `pensieve_external.go`
   向外部 `rl_server` 发送 HTTP 请求
5. `rl_server_no_training.py`
   返回 `0..5` 动作，尾段时返回 `REFRESH`

### 当前兼容层实际做了什么

`pensieve_external.go` 当前只做三件事：

1. 码率映射
   把 GoDASH 本地表示索引按码率升序映射为官方 Pensieve 的 `0..5`
2. 请求封装
   按官方 `rl_server_no_training.py` 使用的字段发送：
   - `lastquality`
   - `buffer`
   - `RebufferTime`
   - `lastChunkStartTime`
   - `lastChunkFinishTime`
   - `lastChunkSize`
   - `lastRequest`
3. 返回处理
   解析服务返回动作；如果收到 `REFRESH`，则保持当前表示索引

### 这个分支的严格限制

当前 `pensieve` 分支有两个硬约束：

1. 当前 `AdaptationSet` 必须恰好有 6 个表示
2. 六档码率升序后必须正好是：
   - `300`
   - `750`
   - `1200`
   - `1850`
   - `2850`
   - `4300` Kbps

如果不满足这两个条件，当前实现不会继续把它当作“官方 Pensieve 一致接入”使用。

### 运行层面的外部限制

虽然 `pensieve` 的 Go 侧接线已经完成，但当前子模块里的官方服务还受这些外部条件限制：

1. `rl_server_no_training.py` 是 Python 2 语法
2. 依赖 TensorFlow 1.x / TFLearn 老环境
3. 当前机器若只有 `python3` 且没有 `tensorflow`，服务无法直接启动

因此这里要明确区分：

- Go 侧接线已经在仓库中
- 服务端能否真正运行，取决于 Pensieve 子模块环境是否满足官方旧依赖

### 2.5.2 每段下载前的分支

`streamLoop(...)` 在每个 segment 开始前会根据 `adapt` 做两件事之一：

1. 普通算法：`accountant.StartTiming()`
2. 跨层 abort 算法：`accountant.SegmentStart_predictStall(...)`

当前分支逻辑是：

- `averageXL`
  只做 `StartTiming()`
- `averageRecentXL`
  只做 `StartTiming()`
- `bba2`
  只做 `StartTiming()`
- `bba2XL-base`
  启动 `SegmentStart_predictStall(...)`
- `bba2XL-rate`
  启动 `SegmentStart_predictStall(...)`
- `bba2XL-double`
  启动 `SegmentStart_predictStall(...)`

所以要分清两类“跨层”：

1. `averageXL` / `averageRecentXL`
   只读取跨层测得吞吐，不做 abort
2. `bba2XL-*`
   在 BBA2 的基础上加入跨层预测和中断下载

### 2.5.3 下载动作本身

真正下载 segment 的地方仍然统一调用：

- `http.GetFile(...)`

大多数算法都复用同一个下载函数，差异不在下载函数本身，而在：

- 下载前是否启用预测
- 下载后如何算下一段 `repRate`

### 2.5.4 下载完成后的算法分派

在 `streamLoop(...)` 的后半段，`switch adapt` 决定下一段表示：

- `averageXL`
  调用 `algo.MeanAverageXLAlgo(...)`
- `averageRecentXL`
  调用 `algo.MeanAverageRecentXLAlgo(...)`
- `bba2`
  调用 `algo.BBA2(...)`
- `bba2XL-base`
  也调用 `algo.BBA2(...)`
- `bba2XL-rate`
  也调用 `algo.BBA2(...)`
- `bba2XL-double`
  也调用 `algo.BBA2(...)`

这说明一个非常重要的事实：

### 论文里的 BBA2-CL / BBA2-CLDouble 并不是换了一个新的“选码率公式”

它们仍然用同一个 `BBA2(...)` 算下一段表示；
区别在于当前段下载过程是否会被跨层预测器提前中断。

也就是说：

- “码率选择逻辑”仍是 BBA2
- “下载控制逻辑”由跨层预测器增强

这正是论文方法的核心。

---

## 2.6 `algorithms/` 里各论文相关算法的具体作用

### 关键文件

- `cross-layer-implementation/godash-qlogabr/algorithms/bba.go`
- `cross-layer-implementation/godash-qlogabr/algorithms/averageXL.go`
- `cross-layer-implementation/godash-qlogabr/algorithms/averageRecentXL.go`
- `cross-layer-implementation/godash-qlogabr/algorithms/helperFunctions.go`

### 2.6.1 `averageXL.go`

`MeanAverageXLAlgo(...)` 的逻辑很简单：

1. 把当前分片应用层吞吐 `newThr` 加入 `thrList`
2. 若样本不足，用普通 throughput 选档
3. 否则从 `XLaccountant.GetAverageThroughput()` 读取跨层平均吞吐
4. 用 `SelectRepRateWithThroughtput(...)` 映射到表示档位

所以：

- `averageXL` 是“平均吞吐算法 + 传输层观测替代应用层估计”

### 2.6.2 `averageRecentXL.go`

与 `averageXL` 唯一主要差异是吞吐来源：

- 用 `GetRecentAverageThroughput()`

也就是最近 3000 包的局部窗口吞吐。

### 2.6.3 `bba.go` 中的 `BBA2(...)`

虽然文件名叫 `bba.go`，但论文真正用的 `BBA2(...)` 函数也在这里。

`BBA2(...)` 的核心特点：

1. 使用 `BBA2Data` 保存跨 segment 状态
2. 用最低码率分片大小列表推导动态 lower reservoir
3. 把 reservoir 写入指标日志：
   - `LOWERRESERVOIR`
4. 根据缓冲区与 reservoir 关系选择目标码率
5. 通过“只允许逐阶升降”的方式限制切换幅度
6. 在 startup / rate-based 与 buffer-based 之间切换时记录：
   - `LOGICSWITCH`

另外还提供：

- `Get_BBA2_LowerReservoir(...)`

这个函数在跨层预测前被调用，用来把当前 lower reservoir 传给 `SegmentStart_predictStall(...)`，让 abort 决策知道当前安全边界在哪里。

### 2.6.4 `helperFunctions.go`

这里有很多算法公用工具，新增算法时经常会复用：

- `CalculateThroughtput(...)`
- `SelectRepRateWithThroughtput(...)`
- 各种平均值函数

如果你的新算法仍然基于“吞吐估计 -> 选择最近不超过的表示”，这个文件基本不用新建很多重复代码。

---

## 2.7 指标日志和图是怎么和算法联动的

### 关键文件

- `cross-layer-implementation/godash-qlogabr/logging/metricLogging.go`
- `paper-utilities/segmentGraph.py`

`metricLogging.go` 会不断输出：

- `BUFFERLEVEL`
- `HIGHESTBANDWIDTH`
- `BUFFERSIZE`
- `STARTTIME`

而 BBA2 与跨层预测会额外输出：

- `LOWERRESERVOIR`
- `LOGICSWITCH`
- `WINDOWTHROUGHPUT`
- `STALLPREDICTOR`

后处理脚本 `segmentGraph.py` 就是依赖这些 tag 来画：

- buffer 曲线
- 码率曲线
- 模拟带宽曲线
- stall predictor 触发点
- BBA reservoir 和逻辑切换线

所以新增算法时，如果你想让图能直接表达算法状态，最好沿用 `MetricLogger` 增加新 tag。

---

## 3. `quic-go/`：跨层通信的底层事件源

## 3.1 这个目录在论文方法中的角色

`quic-go/` 在这里不是让你们去重写 QUIC 协议，而是承担两个作用：

1. 实际通过 HTTP/3 下载 DASH 资源
2. 产生足够细的 qlog 事件给上层 GoDASH 使用

如果没有第二点，`godash-qlogabr` 就只能看到“一个 segment 下载完用了多久”，而看不到 segment 下载过程中的包到达节奏。

---

## 3.2 `quic-go/qlog/qlog.go` 是跨层桥

### 关键文件

- `cross-layer-implementation/quic-go/qlog/qlog.go`
- `cross-layer-implementation/quic-go/qlog/event.go`

### 核心设计

`qlog.NewTracer(getLogWriter, events_crossLayerInput)` 接收两个输出目标：

1. 标准 qlog 文件 writer
2. 一个 `events_crossLayer` channel

然后 `connectionTracer.recordEvent(...)` 每次被调用时会做两件事：

1. `t.events_crossLayer <- Event{...}`
2. `t.events <- Event{...}`

所以所有被 tracer 捕获到的连接事件都会：

- 写入 `.qlog` 文件
- 同时送给 GoDASH 的跨层通道

这是整个跨层设计最关键的一跳。

---

## 3.3 当前 GoDASH 实际消费了哪些 QUIC 事件

### 关键文件

- `cross-layer-implementation/quic-go/qlog/event.go`
- `cross-layer-implementation/godash-qlogabr/crosslayer/crosslayerHelpers.go`

`quic-go/qlog/event.go` 定义了很多事件：

- `EventPacketReceived`
- `EventPacketSent`
- `EventMetricsUpdated`
- `EventPacketLost`
- `EventUpdatedPTO`
- `EventKeyUpdated`
- 等等

但目前 `CrossLayerAccountant.channelListenerThread()` 真正处理的只有：

- `EventPacketReceived`

处理逻辑是：

1. 取 `EventPacketReceived.Length`
2. 追加到 `throughputList`
3. 若处于预测模式，再追加到达时间并调用 `stallPredictor()`

也就是说，当前论文实现主要利用：

- 包到达节奏
- 包大小累计

来推导下载窗口吞吐。

`EventMetricsUpdated` 里的 RTT / cwnd 虽然已经存在于 qlog 事件定义里，但当前 `CrossLayerAccountant` 并没有消费它们。

这也意味着：

如果以后你们想做“RTT-aware ABR”或“cwnd-aware ABR”，大概率不用改 QUIC 主栈太多，只需要：

1. 在 `channelListenerThread()` 中新增对 `EventMetricsUpdated` 的处理
2. 把这些值缓存到 `CrossLayerAccountant`
3. 在新算法里读取这些缓存

---

## 3.4 包接收事件是在哪里发出来的

### 关键文件

- `cross-layer-implementation/quic-go/qlog/qlog.go`

`connectionTracer` 的两个方法：

- `ReceivedLongHeaderPacket(...)`
- `ReceivedShortHeaderPacket(...)`

都会调用：

- `t.recordEvent(time.Now(), &EventPacketReceived{...})`

因此：

- 握手期间的长头包
- 连接建立后的短头包

都会进入跨层通道。

对于当前论文实现来说，这就足够构造“分片下载期间包到达速率”。

---

## 4. 如何切换不同算法

## 4.1 运行时切换入口

算法切换的统一入口是 `adapt` 参数。

来源有三种：

1. 命令行 `-adapt <name>`
2. `godash-qlogabr/config/configure.json` 中的 `"adapt"`
3. 实验框架 `run_endpoint.sh` 里环境变量 `ABR`

### 4.1.1 命令行

例如：

```bash
godash -adapt bba2
godash -adapt bba2XL-base
godash -adapt bba2XL-double
godash -adapt averageXL
```

前提是算法名必须通过 `main.go` 里的 `algorithmSlice` 校验。

### 4.1.2 配置文件

`cross-layer-implementation/godash-qlogabr/config/configure.json`

里面可以直接写：

```json
"adapt": "bba2XL-double"
```

### 4.1.3 Vegvisir / Docker 实验

`cross-layer-implementation/run_endpoint.sh` 会把环境变量 `ABR` 传给：

```bash
godash ... -adapt $ABR ...
```

论文实验矩阵里，就是通过：

- `paper-utilities/vegvisir-configurations/paper_experiment_full.json`

的每个 testcase 中的 `"ABR": "..."` 来切换的。

---

## 4.2 当前可切换的论文相关算法

从代码实现看，论文相关主要有：

- `bba2`
  纯 BBA2
- `bba2XL-base`
  BBA2 + 跨层 abort
- `bba2XL-double`
  BBA2 + 跨层 abort + 下一段低一档可行性预测
- `averageXL`
  平均吞吐算法，吞吐来源换成跨层包级估计
- `averageRecentXL`
  近期窗口跨层吞吐版本

理论上还实现了：

- `bba2XL-rate`

但当前不能正常切换，原因是前面提到的 `algorithmSlice` 漏项。

---

## 5. 如果要新增一个 ABR 算法，应该改哪些地方

下面按“最小改动路径”给出。

## 5.1 最基础：新增一个纯 GoDASH 层算法

假设你要新增算法 `myabr`，且它只依赖：

- bufferLevel
- throughput
- bandwithList

不依赖新的 QUIC 事件。

### 需要修改的文件

1. `cross-layer-implementation/godash-qlogabr/global/globalVar.go`
   新增常量：
   `const MyABRAlg = "myabr"`

2. `cross-layer-implementation/godash-qlogabr/main.go`
   修改：
   - `algorithmSlice`
   - `-adapt` 帮助字符串

3. `cross-layer-implementation/godash-qlogabr/algorithms/`
   新建例如：
   - `myabr.go`
   并实现例如：
   `func MyABR(...) int`

4. `cross-layer-implementation/godash-qlogabr/player/player.go`
   至少要改两个 `switch adapt`：
   - 分片开始前的准备逻辑
   - 分片下载后的表示选择逻辑

### 在 `player.go` 里至少需要处理的点

#### 点 1：下载开始前

如果算法只需要普通吞吐统计：

- 进入 `case glob.MyABRAlg:`
- 调用 `accountant.StartTiming()`

如果算法不需要跨层信息，也可以完全不碰 `accountant`，但通常保留 `StartTiming()` 更方便后续扩展。

#### 点 2：下载完成后选下一段

在后半段的 `switch adapt` 中加：

```go
case glob.MyABRAlg:
    repRate = algo.MyABR(...)
```

#### 点 3：如果算法需要自己的状态

像 BBA2 一样，如果你有跨段状态，需要：

- 在 `player.go` 顶部定义状态结构变量
- 在 `Stream(...)` 开始时初始化
- 每次 segment 决策时传进去

### 可能还要改的文件

- `cross-layer-implementation/godash-qlogabr/README.md`
  更新算法列表
- `cross-layer-implementation/godash-qlogabr/config/configure.json`
  如果你想把它作为默认算法或测试配置
- `paper-utilities/vegvisir-configurations/paper_experiment_full.json`
  如果你要把它加入论文实验矩阵

---

## 5.2 如果新增的是“跨层感知但不需要新 QUIC 事件”的算法

比如你要做：

- 基于最近 N 个 QUIC 包平均速率的吞吐算法
- 基于现有 stall predictor 的变体

这种情况，`quic-go/` 往往不用改。

### 只需要改 GoDASH 侧

1. 复用 `CrossLayerAccountant` 已有方法：
   - `GetAverageThroughput()`
   - `GetRecentAverageThroughput()`
   - `SegmentStart_predictStall(...)`
2. 在 `player.go` 里决定：
   - 只 `StartTiming()`
   - 还是进入 `SegmentStart_predictStall(...)`
3. 在 `algorithms/` 里实现新算法

这类算法本质上只是“消费方式不同”，不需要 QUIC 层新接口。

---

## 5.3 如果新增的是“需要新的 QUIC 指标”的算法

比如你们想做：

- RTT-aware ABR
- cwnd-aware ABR
- packet loss-aware ABR
- PTO-aware ABR

那就需要同时动 `quic-go/` 和 `godash-qlogabr/`。

### 方案 A：直接复用现有 qlog 事件

先确认你要的指标是否已经在 `quic-go/qlog/event.go` 里有。

当前已有：

- `EventMetricsUpdated`
  包含 `min_rtt`、`smoothed_rtt`、`latest_rtt`、`rtt_variance`、`congestion_window`、`bytes_in_flight`
- `EventPacketLost`
- `EventUpdatedPTO`

如果你要的就是这些，通常只需要：

1. 修改 `crosslayer/crosslayerHelpers.go`
2. 在 `channelListenerThread()` 中识别这些事件
3. 把它们缓存到 `CrossLayerAccountant`
4. 给新算法提供 getter

### 方案 B：需要全新事件

如果现有 qlog 事件不够，就要扩展 `quic-go/qlog/`：

1. 在 `cross-layer-implementation/quic-go/qlog/event.go`
   新增事件结构和 `EventType()`
2. 在 `cross-layer-implementation/quic-go/qlog/qlog.go`
   找到合适的 tracer hook 发出事件
3. 在 `cross-layer-implementation/godash-qlogabr/crosslayer/crosslayerHelpers.go`
   增加消费逻辑
4. 在 `algorithms/` 新算法中读取这些新信号

注意：这一步通常不需要改 QUIC 状态机主逻辑，重点是扩展 tracer 输出。

---

## 5.4 新增算法的最小清单

如果让我给一个最小 checklist，会是：

1. `global/globalVar.go`
   增加算法名常量
2. `main.go`
   把算法加入 `algorithmSlice`
3. `main.go`
   更新 `-adapt` 帮助字符串
4. `algorithms/`
   新增算法实现文件
5. `player/player.go`
   在“下载前准备”分支里接入
6. `player/player.go`
   在“下载后选表示”分支里接入
7. 如需跨段状态：
   在 `player.go` 初始化并传入
8. 如需跨层信号：
   决定复用现有 `CrossLayerAccountant` 还是扩展它
9. 如需新 QUIC 指标：
   扩展 `quic-go/qlog/`
10. 如需实验复现：
   更新 `configure.json` / `paper_experiment_full.json`

---

## 6. 对论文现有实现的准确理解

这里特别强调三点，避免后续误读代码。

### 6.1 BBA2-CL 不是独立的“新 BBA2 公式文件”

`bba2XL-base` / `bba2XL-double` 最终还是走同一个：

- `algo.BBA2(...)`

它们与 `bba2` 的核心差异不在“下一段怎么选”，而在：

- 当前段下载过程中，是否允许根据传输层包级迹象提前中止当前下载

### 6.2 跨层信息的主要来源是“包到达速率”，不是 RTT

当前实现实际消费的是：

- `EventPacketReceived.Length`

因此这个论文版本的跨层信息主要代表：

- 接收节奏
- 实时有效吞吐

而不是：

- RTT 直接反馈
- cwnd 直接反馈

这些指标在 qlog 里有定义，但当前没有进入算法决策。

### 6.3 `averageXL` 与 `bba2XL-*` 是两种不同风格的跨层算法

- `averageXL`
  跨层信号直接替代 throughput estimator
- `bba2XL-*`
  仍用 BBA2 选档，但用跨层预测器控制当前段下载是否应被中断

这两类方法的代码入口不同，不要混在一起理解。

---

## 7. 建议你们下一步先做的修正

如果接下来要继续在这个仓库上做论文扩展，我建议先修这两个点：

1. 修复 `bba2XL-rate` 的可选性
   需要把 `glob.BBA2Alg_AVXL_rate` 加入 `main.go` 的 `algorithmSlice`，并更新 `-adapt` 帮助字符串。
2. 把 `player.go` 里的算法分支抽成函数表或 dispatcher
   现在多个 `switch adapt` 重复较多，新增算法时很容易漏改。

第二点不是必须，但如果你们要频繁尝试新 ABR，会明显减少出错概率。

---

## 8. 直接回答你们最关心的三个问题

### 8.1 论文 ABR 算法怎么实现

实现主线在：

- `godash-qlogabr/player/player.go`
- `godash-qlogabr/algorithms/bba.go`
- `godash-qlogabr/algorithms/averageXL.go`
- `godash-qlogabr/algorithms/averageRecentXL.go`

其中：

- `bba2` 用 `BBA2(...)`
- `bba2XL-*` 仍用 `BBA2(...)`，但加上跨层 abort
- `averageXL` / `averageRecentXL` 直接读取跨层估算吞吐选档

### 8.2 跨层通信怎么实现

实现主线在：

- `godash-qlogabr/main.go`
- `godash-qlogabr/http/urlParsing.go`
- `quic-go/qlog/qlog.go`
- `quic-go/qlog/event.go`
- `godash-qlogabr/crosslayer/crosslayerHelpers.go`

数据流是：

`QUIC packet event -> qlog tracer -> EventChannel -> CrossLayerAccountant -> ABR / stall predictor`

### 8.3 如果新增算法怎么改

最少改这四类文件：

- `global/globalVar.go`
- `main.go`
- `algorithms/<new>.go`
- `player/player.go`

如果只是复用现有跨层吞吐，不必改 `quic-go/`。
如果要读 RTT / cwnd / loss 等新信号，再扩展 `CrossLayerAccountant`，必要时扩展 `quic-go/qlog/`。
