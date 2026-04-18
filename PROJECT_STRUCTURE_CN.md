# 项目结构说明

本文档用于说明仓库 `cross-that-boundary-mmsys23-nossdav` 的目录结构、关键文件作用，以及实验日志目录的组织规律。

## 1. 项目定位

这是 NOSSDAV 2023 论文《Cross that boundary: Investigating the feasibility of cross-layer information sharing for enhancing ABR decision logic over QUIC》的复现实验仓库。当前仓库主体由四部分组成：

1. `cross-layer-implementation/`
   包含论文定制的 GoDASH 客户端和 quic-go 实现，是核心源码。
2. `tc-netem-shaper/`
   包含网络仿真与整形容器，用 `tc netem` 复现实验中的带宽变化。
3. `paper-utilities/`
   包含 Vegvisir 环境脚本、MPD 转换脚本、图表生成脚本，以及后来接入的 `pensieve` 子模块。
4. `paper-logs/`
   包含论文实验产物，主要是客户端日志、qlog、QoE 结果、图表和整形器日志。

## 2. 顶层结构

```text
.
├── README.md
├── LICENSE.md
├── .gitignore
├── .gitmodules
├── ABR_CROSSLAYER_IMPLEMENTATION_CN.md
├── PENSIEVE_IMPLEMENTATION_CHANGES_CN.md
├── PENSIEVE_REPRODUCTION_PLAN_CN.md
├── PROJECT_STRUCTURE_CN.md
├── cross-layer-implementation/
├── paper-logs/
├── paper-utilities/
└── tc-netem-shaper/
```

### 顶层文件

- `README.md`
  仓库总入口。说明论文背景、复现实验步骤、Vegvisir 集成方式、数据集准备流程，以及 `paper-logs/` 的命名规则。
- `LICENSE.md`
  仓库整体许可证文本。
- `.gitignore`
  Git 忽略规则。
- `.gitmodules`
  Git 子模块定义文件。当前主要用于记录 `paper-utilities/pensieve` 的远程地址。
- `ABR_CROSSLAYER_IMPLEMENTATION_CN.md`
  说明 `godash-qlogabr` 与 `quic-go` 中 ABR 和跨层通信的实现链路。
- `PENSIEVE_IMPLEMENTATION_CHANGES_CN.md`
  说明当前仓库如何通过 `pensieve` 子模块对接官方 `rl_server`。
- `PENSIEVE_REPRODUCTION_PLAN_CN.md`
  说明为什么 Pensieve 复现方案调整为“子模块 + 播放器侧兼容层”。
- `PROJECT_STRUCTURE_CN.md`
  本文档自身。

## 3. `cross-layer-implementation/`：核心实现

该目录同时打包了两个项目：

- 定制版 `godash-qlogabr`：ABR 客户端逻辑、跨层决策、指标记录
- 定制版 `quic-go`：底层 QUIC / HTTP/3 传输与 qlog 事件输出

### 3.1 目录概览

```text
cross-layer-implementation/
├── Dockerfile
├── run_endpoint.sh
├── godash-qlogabr/
└── quic-go/
```

### 3.2 顶层文件说明

- `cross-layer-implementation/Dockerfile`
  构建实验客户端容器。第一阶段编译本仓库中的 `quic-go` 与 `godash-qlogabr`，第二阶段基于 `martenseemann/quic-network-simulator-endpoint` 安装 `itu-p1203`，拷贝 `godash` 二进制并设置入口脚本。
- `cross-layer-implementation/run_endpoint.sh`
  容器运行入口。根据环境变量为 Vegvisir 端点配置 GoDASH 参数，在 `ROLE=client` 时启动带 QUIC 和 QoE 记录能力的 `godash`。

---

## 4. `cross-layer-implementation/godash-qlogabr/`：论文定制的 GoDASH 客户端

这是仓库中最直接体现论文算法的部分。它在原始 GoDASH 基础上加入：

- 论文中的 BBA2 / BBA2-CL / BBA2-CLDouble 适配逻辑
- 基于 qlog 事件的跨层信息共享
- 额外指标日志
- QoE 后处理支持

### 4.1 根目录文件

- `main.go`
  GoDASH 主程序入口。负责解析命令行、读取配置、初始化 qlog 事件通道、启动 `CrossLayerAccountant`、加载 MPD 和调用播放器主流程。
- `go.mod`
  Go 模块定义，声明依赖。
- `README.md`
  该子项目的使用说明，包含支持的 ABR 算法、构建运行方式和参数说明。
- `ABR.md`
  各 ABR 算法简述，尤其说明 BBA2 等策略。
- `PROBLEMS.md`
  记录该项目已知问题或历史问题说明。
- `VERSION`
  版本号标记。
- `LICENSE`
  GoDASH 子项目许可证。
- `.gitignore`
  子项目忽略规则。

### 4.2 子目录说明

#### `algorithms/`

该目录存放自适应码率算法实现。

- `arbiterPlus.go`
  Arbiter+ 算法。
- `average.go`
  平均吞吐驱动的 ABR。
- `averageXL.go`
  使用跨层信息的平均吞吐变体。
- `averageRecentXL.go`
  使用更近期窗口和跨层信息的平均吞吐变体。
- `bba.go`
  基础 BBA 算法。
- `bba2.go`
  论文主要使用的 BBA2 及其相关逻辑。
- `conventional.go`
  常规吞吐估计式 ABR。
- `elastic.go`
  Elastic 算法。
- `exponential.go`
  指数平滑吞吐算法。
- `geometric.go`
  几何平均吞吐算法。
- `logistic.go`
  Logistic 型算法。
- `helperFunctions.go`
  算法间共用辅助函数。
- `pensieve_external.go`
  当前仓库新增的 Pensieve 外部客户端兼容层。它不实现策略本身，只负责把 GoDASH 当前状态转成官方 `rl_server` 可接收的请求，并把返回的动作索引映射回本地表示索引。
- `average_test.go`
  平均吞吐算法的单元测试。

#### `config/`

- `configure.json`
  默认配置文件。
- `configure-godashbed-quic.json`
  针对 QUIC / 测试床环境的配置模板。
- `configure-godashbed-tcp.json`
  针对 TCP / 测试床环境的配置模板。
- `configure_old_working.json`
  旧版可工作配置备份。
- `MPD_test_URLS`
  用于测试的 MPD 地址列表。

#### `crosslayer/`

- `crosslayerHelpers.go`
  跨层核心逻辑。定义 `CrossLayerAccountant`，消费 qlog 事件，维护吞吐时间窗，估算当前下载进度，触发 stall predictor 和 abort 决策，并向 `metrics_log.txt` 输出跨层指标。

#### `evaluate/`

- `test_goDASH.py`
  批量运行 GoDASH 客户端的评估脚本。
- `config/`
  评估脚本使用的配置目录。
- `urls/`
  评估 URL 列表目录。
- `.gitignore`
  该目录忽略规则。

#### `global/`

- `globalVar.go`
  全局常量与默认参数定义，如算法名、文件路径、日志开关名称等。

#### `hlsfunc/`

- `hlsFunctions.go`
  HLS / 渐进下载相关辅助逻辑。

#### `http/`

- `mpdParsing.go`
  解析 MPD、构建流表示与分片信息。
- `urlParsing.go`
  URL 处理和下载地址生成。
- `certs/`
  测试环境用证书目录。

#### `logging/`

- `configParsing.go`
  解析配置文件。
- `debug.go`
  调试日志输出。
- `metricLogging.go`
  指标日志器。周期性记录 `BUFFERLEVEL` 等事件，并写入 `metrics_log.txt`。

#### `player/`

- `player.go`
  播放核心流程。负责下载分片、调用 ABR 算法、更新缓冲区、记录 QoE、调用跨层预测器，是 GoDASH 的主业务逻辑。

#### `qlog/`

这是 GoDASH 自己的 qlog / 状态跟踪层，不同于 `quic-go/qlog/`。

- `event.go`
  事件定义。
- `perspective.go`
  客户端 / 服务端视角定义。
- `qlog.go`
  qlog 主体封装。
- `rtt_stats.go`
  RTT 统计结构。
- `sid.go`
  追踪标识辅助。
- `singleton.go`
  单例跟踪器。
- `stats.go`
  统计信息结构。
- `trace.go`
  trace 组织逻辑。
- `types.go`
  通用类型定义。

#### `qoe/`

QoE 计算模型实现。

- `claye.go`
  Claye QoE 模型。
- `duanmu.go`
  Duanmu 模型。
- `p1203.go`
  ITU P.1203 相关封装。
- `qoe.go`
  QoE 统一入口。
- `qoe6.go`
  额外 QoE 逻辑。
- `yin.go`
  Yin 模型。
- `yu.go`
  Yu 模型。

#### `utils/`

- `appFunctions.go`
  应用级辅助函数。
- `calcFunctions.go`
  计算辅助函数。

#### `P2Pconsul/`

协作式客户端功能，论文主线不是这里，但保留了原始项目能力。

- `NodeUrl.go`
  节点地址与协作信息。
- `HelperFunctions/`
  协作辅助函数目录。
- `P2PService/`
  协作服务目录。

### 4.3 这一子项目的功能主线

可以把 `godash-qlogabr/` 看成以下几层：

1. `main.go`
   负责把命令行、配置、日志、qlog 和播放器组装起来。
2. `player/player.go`
   负责执行下载与播放仿真。
3. `algorithms/*.go`
   负责在每个分片到来前选择码率。
4. `crosslayer/crosslayerHelpers.go`
   负责从 QUIC qlog 事件中提取跨层信息，并在必要时触发提前中断。
5. `logging/metricLogging.go`
   把缓冲区、窗口吞吐、stalls、reservoir 等关键指标落盘，供后续绘图和 QoE 分析。

---

## 5. `cross-layer-implementation/quic-go/`：定制的 QUIC / HTTP3 实现

该目录是论文使用的 QUIC 栈。整体结构基本继承上游 `quic-go`，但仓库目的不是开发一个新协议库，而是让 GoDASH 通过 qlog 获得更细粒度的传输层事件。

### 5.1 目录概览

```text
cross-layer-implementation/quic-go/
├── 根目录 QUIC 连接与流实现
├── http3/
├── internal/
├── logging/
├── qlog/
├── example/
├── integrationtests/
├── fuzzing/
├── interop/
└── docs/
```

### 5.2 根目录关键文件

根目录大部分文件都属于 QUIC 连接主实现，命名非常直接，可按职责理解：

- `client.go`
  QUIC 客户端拨号入口，如 `DialAddr`、`Dial`、`DialEarly`。
- `server.go`
  QUIC 服务端入口。
- `connection.go`
  连接主状态机，维护握手、收发包、流、多路复用、拥塞与计时器。
- `config.go`
  配置对象与默认配置校验。
- `interface.go`
  对外暴露的接口类型。
- `send_stream.go` / `receive_stream.go` / `stream.go`
  发送流、接收流和统一流封装。
- `streams_map*.go`
  流 ID 到流对象的管理。
- `packet_packer.go` / `packet_unpacker.go`
  QUIC 报文打包与解包。
- `framer.go`
  将 frame 组织进报文。
- `retransmission_queue.go`
  重传队列。
- `connection_timer.go`
  连接定时器。
- `send_queue.go`
  发送队列。
- `window_update_queue.go`
  流控窗口更新队列。
- `datagram_queue.go`
  Datagram 扩展相关队列。
- `token_store.go`
  Token 保存。
- `multiplexer.go`
  多连接复用。
- `buffer_pool.go`
  缓冲池。
- `errors.go`
  错误定义。
- `mtu_discoverer.go`
  DPLPMTUD 路径 MTU 探测。
- `sys_conn*.go`
  不同平台上的 UDP / OOB / DF 支持。
- `closed_conn.go`
  已关闭连接的占位处理。
- `zero_rtt_queue.go`
  0-RTT 队列。

### 5.3 测试与生成文件规律

这一目录里存在大量测试与 mock 文件，它们不是论文逻辑本身，但有助于维护 QUIC 栈完整性：

- `*_test.go`
  单元测试或集成测试。
- `mock_*.go`
  测试桩 / mock。
- `mockgen.go`、`mockgen_private.sh`
  mock 生成相关文件。
- `.golangci.yml`、`codecov.yml`
  静态检查与覆盖率配置。
- `.circleci/`、`.github/`
  CI 配置。

### 5.4 与论文最相关的目录

#### `qlog/`

这是论文跨层设计最相关的部分。

- `qlog.go`
  创建 tracer，并把 qlog 事件同时写到标准 qlog 输出和额外的 `events_crossLayer` 通道；这是 GoDASH 获取 QUIC 事件的桥梁。
- `event.go`
  qlog 事件定义，含 RTT、连接、包级信息。
- `trace.go`
  qlog 顶层 trace 结构。
- `types.go`
  公共类型。
- `frame.go`
  帧级序列化。
- `packet_header.go`
  包头序列化。
- 对应的 `*_test.go`
  qlog 模块测试。

#### `logging/`

- `interface.go`
  定义 tracer / connection tracer 接口，`qlog.NewTracer(...)` 通过这里接入整个连接生命周期。

#### `http3/`

实现 HTTP/3 客户端和服务端，是 GoDASH 通过 QUIC 拉取 DASH 资源的应用层。

- `client.go`
  HTTP/3 客户端。
- `server.go`
  HTTP/3 服务端。
- `roundtrip.go`
  RoundTripper 实现。
- `request.go` / `request_writer.go`
  请求解析与伪头生成。
- `response_writer.go`
  响应写回。
- `http_stream.go`
  HTTP/3 stream 封装。
- `frames.go` / `capsule.go`
  HTTP/3 帧与 capsule。
- `body.go`
  响应体处理。
- `error_codes.go`
  HTTP/3 错误码。
- `gzip_reader.go`
  gzip 响应读取支持。

#### `internal/`

这是 quic-go 内部实现层，按组件分目录：

- `internal/ackhandler/`
  ACK 处理与丢包恢复。
- `internal/congestion/`
  拥塞控制。
- `internal/flowcontrol/`
  流控。
- `internal/handshake/`
  TLS 握手、密钥安装、0-RTT / 1-RTT 切换。
- `internal/protocol/`
  协议常量和底层类型。
- `internal/qerr/`
  QUIC 错误码。
- `internal/qtls/`
  qtls 适配。
- `internal/utils/`
  时间、日志、缓冲等通用工具。
- `internal/wire/`
  QUIC 帧与包头编解码。
- `internal/logutils/`
  日志辅助。
- `internal/mocks/`
  内部模块测试用 mock。
- `internal/testdata/`、`internal/testutils/`
  测试资源。

#### 其他目录

- `example/`
  官方示例客户端和回显服务。
- `integrationtests/`
  集成测试。
- `fuzzing/`
  模糊测试。
- `interop/`
  与其他 QUIC 实现互通测试代码。
- `docs/`
  文档资源。
- `quicvarint/`
  QUIC 可变长整数实现。

### 5.5 如何理解这个子项目

`quic-go/` 在本仓库中的价值不是“需要你逐文件修改”，而是“为上层 GoDASH 提供带 qlog 细节的传输事件源”。论文相关主线是：

1. HTTP/3 下载分片
2. qlog 记录连接和包级事件
3. 这些事件被 `events_crossLayer` 转发给 GoDASH
4. GoDASH 根据传输层实时状况调整或中断当前下载

---

## 6. `paper-utilities/`：实验辅助脚本

```text
paper-utilities/
├── Convert_to_BBA2.py
├── segmentGraph.py
├── visualize_ouput.html
├── Visualizations/
├── pensieve/
├── vegvisir-configurations/
└── vegvisir-scripts/
```

### 文件与目录说明

- `Convert_to_BBA2.py`
  把原始 DASH 数据集中的 `simple.mpd` 转换成论文算法可用的 MPD。脚本会扫描每个表示的分片文件大小，计算 `maxAvgRatio`，并把 `chunks` 列表与新的 `SegmentTemplate` 写回到新 MPD 中。
- `segmentGraph.py`
  从 `metrics_log.txt` 和 `shaper_metrics.txt` 读取时间序列，绘制缓冲区、码率选择、模拟吞吐、stall predictor 触发点等图形。
- `visualize_ouput.html`
  一个静态 HTML 浏览页，用于批量显示各实验目录中的 `viz_stallprediction.png` 和 `itu-p1203.json` 分数。

### `pensieve/`

- 这是后续加入的 Git 子模块，远程位于 `https://github.com/123131513/pensieve`。
- 目录内容基本保持官方 `hongzimao/pensieve` 结构，包含：
  - `rl_server/`
    官方在线推理服务脚本与预训练 checkpoint
  - `sim/`
    模拟训练与测试脚本
  - `test/`
    离线评测脚本
  - `real_exp/`、`run_exp/`
    官方实验驱动脚本
- 当前仓库不再维护 `pensieve_service.py` 副本，而是通过 `cross-layer-implementation/godash-qlogabr/algorithms/pensieve_external.go` 与这个子模块中的 `rl_server/rl_server_no_training.py` 对接。
- 需要注意：
  - 该子模块中的 `rl_server_no_training.py` 是旧版 Python 2 / TensorFlow 1.x 代码
  - 当前机器若只有 `python3`，不能直接运行它

### `vegvisir-scripts/`

- `cl.py`
  Vegvisir 的 CrossLayer 环境脚本。实验结束后自动调用 `segmentGraph.py` 生成图像，并对最后一个分片 JSON 运行 `itu_p1203`，把结果写为 `itu-p1203.json`。
- `__init__.py`
  Python 包初始化文件。

### `vegvisir-configurations/`

- `implementations.json`
  声明 Vegvisir 可用的 client、shaper、server 镜像及参数模式。
- `paper_experiment_full.json`
  论文完整实验矩阵，枚举三类数据集、三种分片时长和三种 ABR 变体的测试项。

### `Visualizations/`

- `requirements.txt`
  可视化依赖列表。

---

## 7. `tc-netem-shaper/`：网络整形器

```text
tc-netem-shaper/
├── Dockerfile
├── run.sh
├── scenarios/
└── wait-for-it-quic/
```

### 文件与目录说明

- `Dockerfile`
  构建网络整形器镜像。安装 `iproute2`、`iptables`、`tcpdump`、`netcat` 等工具，并编译 `wait-for-it-quic`。
- `run.sh`
  容器入口脚本。配置两块接口、切换混杂模式、等待服务端可用、通过 `netcat` 做同步，然后执行指定的 `scenarios/*.sh`。

### `scenarios/`

- `bba_buffering_paper.sh`
  论文实验网络轨迹。按时间片修改 `tc qdisc`，依次设置 2000/100/1000/100/2000 kbps 等带宽，并把每次设置记录到 `/logs/shaper_metrics.txt`。

### `wait-for-it-quic/`

- `wait-for-it.go`
  一个面向 QUIC 的“等待服务端上线”工具。它发送会触发 Version Negotiation 的 UDP 探测包，只要收到预期响应，就认为对端已经可达。
- `go.mod`
  该小工具的 Go 模块文件。

---

## 8. `paper-logs/`：论文实验结果目录

这是仓库里文件数量最多的部分。它不是源码，而是实验产物归档。

### 8.1 命名规则

每个实验目录遵循：

```text
godashcl-{algorithm}-{dataset}-{segment}__tc-netem-cl-paper__quic-go
```

其中：

- `algorithm`
  - `bba2`
  - `bba2cl`
  - `bba2cl-double`
- `dataset`
  - `bbb` = Big Buck Bunny
  - `ed` = Elephants Dream
  - `ofm` = Of Forest And Men
- `segment`
  - `2s`
  - `4s`
  - `6s`

这意味着 `paper-logs/` 本质上保存的是一个 `3 x 3 x 3 = 27` 个实验组合的结果集。

### 8.2 单个实验目录模板

以 `paper-logs/godashcl-bba2-bbb-2s__tc-netem-cl-paper__quic-go/` 为例：

```text
paper-logs/<case>/
├── output.txt
├── client/
├── server/
└── shaper/
```

#### 实验目录顶层文件

- `output.txt`
  Vegvisir / 环境运行时的整体控制台输出，通常包含环境准备、路由、容器执行信息。

#### `client/` 目录

这里保存 GoDASH 客户端侧产物：

- `godash.log.txt`
  GoDASH 调试日志。
- `metrics_log.txt`
  由 `logging/metricLogging.go` 和跨层逻辑生成的指标时序日志。
- `itu-p1203.json`
  由 `vegvisir-scripts/cl.py` 后处理得到的 ITU P.1203 QoE 结果。
- `Client_abr_(empty).qlog`
  客户端 ABR 侧 qlog 占位文件或空 trace。
- `client_<id>.qlog`
  客户端 QUIC 连接 qlog。
- `viz_none.png`
  不附加额外标记的图。
- `viz_stallprediction.png`
  标出 stall predictor 触发点的图。
- `viz_bba.png`
  标出 BBA reservoir / 逻辑切换的图。
- `viz_command_none.txt`
  生成 `viz_none.png` 时使用的命令。
- `viz_command_stallprediction.txt`
  生成 `viz_stallprediction.png` 时使用的命令。
- `viz_command_bba.txt`
  生成 `viz_bba.png` 时使用的命令。

##### `client/files/`

这里是按分片保存的下载结果和单分片 QoE JSON：

- `logDownload.txt`
  下载文件列表或下载日志。
- `2sec_isoff-live_BigBuckBunny_2s1.m4s.json` 这类文件
  针对单个下载分片的 QoE / 媒体元数据输出。文件名中会编码数据集名、分片时长和分片编号。

这一类文件在各实验中数量很多，但内容模式一致，本质上是“每个分片一个 JSON 结果文件”。

#### `server/` 目录

- `log.txt`
  服务端日志。
- `qlog/<id>.qlog`
  服务端 QUIC 连接 qlog。

#### `shaper/` 目录

- `shaper_metrics.txt`
  由 `bba_buffering_paper.sh` 输出的网络整形时间序列，如 `SIMULATIONTHROUGHPUT`。

### 8.3 为什么这里不逐个解释每个 `.m4s.json`

`paper-logs/` 下大量文件是自动生成、同构且仅实验参数不同的产物，逐个单独解释价值很低。更有效的理解方式是：

1. 先看实验目录名，确定算法 / 数据集 / 分片时长
2. 再看 `client/`、`server/`、`shaper/` 三方日志
3. 最后按文件模式理解：
   - `*.qlog` 是传输层 trace
   - `metrics_log.txt` 是客户端时序指标
   - `shaper_metrics.txt` 是网络轨迹
   - `itu-p1203.json` 是 QoE 汇总
   - `viz_*.png` 是图形化结果
   - `client/files/*.m4s.json` 是单分片明细

---

## 9. 建议的阅读顺序

如果你是第一次接触这个仓库，推荐按下面顺序阅读：

1. `README.md`
   先理解论文实验目标与运行方式。
2. `paper-utilities/vegvisir-configurations/paper_experiment_full.json`
   看清楚实验矩阵是如何枚举的。
3. `cross-layer-implementation/run_endpoint.sh`
   理解实验客户端容器如何启动 GoDASH。
4. `cross-layer-implementation/godash-qlogabr/main.go`
   看 GoDASH 程序如何初始化。
5. `cross-layer-implementation/godash-qlogabr/player/player.go`
   看主播放与分片下载流程。
6. `cross-layer-implementation/godash-qlogabr/crosslayer/crosslayerHelpers.go`
   看跨层共享与提前中断的关键逻辑。
7. `cross-layer-implementation/quic-go/qlog/qlog.go`
   看 QUIC 事件如何被转发给上层。
8. `paper-utilities/segmentGraph.py`
   看实验图是如何由指标日志生成的。
9. `paper-logs/<任一实验目录>/`
   对照源码理解实验输出。

---

## 10. 总结

这个仓库不是一个单体应用，而是一套完整的论文复现实验资产，结构上可以概括为：

- `cross-layer-implementation/`
  论文方法本身
- `tc-netem-shaper/`
  实验网络条件
- `paper-utilities/`
  实验运行与后处理工具
- `paper-logs/`
  论文结果归档

如果只关心“论文方法实现在哪”，重点看：

- `cross-layer-implementation/godash-qlogabr/crosslayer/crosslayerHelpers.go`
- `cross-layer-implementation/godash-qlogabr/player/player.go`
- `cross-layer-implementation/godash-qlogabr/algorithms/bba2.go`
- `cross-layer-implementation/quic-go/qlog/qlog.go`

如果只关心“实验结果怎么看”，重点看：

- `paper-logs/`
- `paper-utilities/segmentGraph.py`
- `paper-utilities/visualize_ouput.html`
