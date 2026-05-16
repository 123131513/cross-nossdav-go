# GoDASH 接入 MASQUE 的最小实施与最小运行验证记录

## 1. 文档目的

本文档记录两部分内容：

1. 按照“最小实施顺序”对当前 GoDASH 客户端做了哪些改动。
2. 基于本地 `has-quicgo` media server 与 `masque-go-original` proxy，进行了怎样的最小运行验证，以及验证结果是什么。

本文档只覆盖第一阶段最小接入，不覆盖完整 tunnel-aware ABR 设计，也不覆盖 CONNECT-IP / migration / forwarded mode。

## 1.1 当前最终结论

截至本文档最后一次更新，当前状态不是“还在尝试接入”，而是：

1. GoDASH 已完成 `direct` / `masque` 双路径切换入口。
2. `GoDASH client -> MASQUE proxy -> media server` 最小链路已经打通。
3. MASQUE 模式下已经成功获取 MPD、init segment 和前两个 media segment。
4. `metrics_log.txt`、客户端 qlog、下载文件都已经出现。
5. 当前剩余工作已经从“最小联通”转向“实验化整理、参数收敛、指标扩展”。

为避免阅读时混淆，本文第 8 到第 12 节记录的是“第一次最小验证时的失败状态”，第 13 节以后记录的是“继续协议级调试后最终跑通的状态”。

## 1.2 多模式接入更新

在最小 `direct / masque` 双路径基础上，当前 GoDASH 已继续接入 TECC fork 中的多种隧道模式。`masque` 保留为兼容旧实验的别名，语义等同于 MDT baseline。

当前可用 `-transport`：

- `direct`：原始 HTTP/3 直连，不经过 MASQUE/TECC 隧道。
- `masque`：兼容别名，等同于 `mdt`。
- `mdt`：MASQUE Datagram Tunnel，外层走 QUIC DATAGRAM。
- `mst`：MASQUE Stream Tunnel，外层走 QUIC stream。
- `mft`：forwarding baseline，当前在 GoDASH 侧按 direct-like forwarding baseline 记录模式。
- `rmdt`：MDT + tunnel-side retransmission。
- `rmdt-bbr`：RMDT + tunnel-side CC 实验标签；实际 tunnel CC 仍主要由 TECC proxy 启动参数控制。
- `tecc`：RMDT + TECC feedback forwarding，用于 full/lite TECC 实验。

依赖切换：

- GoDASH 现在通过 `go.mod` 使用 `/home/hellodaniel0/TECC/forks/masque-go-tecc`：

```text
replace github.com/quic-go/masque-go => ../../../TECC/forks/masque-go-tecc
```

### 如何切换模式

所有 MASQUE/TECC 类模式都需要 `-quic on` 和 `-masqueProxyTemplate`。只切换 `-transport` 即可更换客户端模式：

```bash
./godash \
  -url "[https://127.0.0.1:4433/moi.mpd]" \
  -quic on \
  -transport mdt \
  -masqueProxyTemplate "https://127.0.0.1:5543/masque?h={target_host}&p={target_port}" \
  -adapt bba2 \
  -streamDuration 120 \
  -debug on \
  -terminalPrint on
```

把上面的 `-transport mdt` 换成以下任一值即可：

```text
direct
masque
mdt
mst
mft
rmdt
rmdt-bbr
tecc
```

注意：

- `direct` 和当前 `mft` 不需要 `-masqueProxyTemplate`。
- `masque` 与 `mdt` 都是 datagram tunnel baseline，建议新实验使用 `mdt`，旧脚本可继续使用 `masque`。
- `mst / rmdt / tecc` 需要 TECC 版 proxy 侧用对应参数启动，否则 client 侧模式与 proxy 侧能力不匹配会失败或退化。
- `rmdt-bbr` 只是 GoDASH 侧的模式标签；是否启用 tunnel-side BBR/CC 取决于 TECC proxy 的启动参数。

### TECC/RMDT 指标采集

GoDASH 已新增以下原始日志字段，可通过 `-printHeader` 选择输出：

```text
TunnelRetrans
TunnelRetransRate
TunnelQueueBytes
TunnelBwEstimate
TunnelTargetRate
TunnelFeedbackRTT
ServerSendRate
TunnelForwardRate
```

推荐 TECC/RMDT 实验额外传入 proxy debug URL：

```bash
-teccProxyDebugURL "https://127.0.0.1:5543/debug/rmdt"
```

TECC full/lite 实验如果需要 client 转发 tunnel feedback 到 content server，再传：

```bash
-teccFeedbackForwardURL "http://127.0.0.1:8090/feedback"
```

指标来源说明：

- `TunnelRetrans / TunnelRetransRate` 来自 TECC proxy debug JSON 的 RMDT retransmission counters。
- `TunnelQueueBytes / TunnelBwEstimate / TunnelForwardRate / TunnelFeedbackRTT` 来自 TECC feedback/debug snapshot；队列字段按 tunnel packet 数近似换算为 bytes。
- `ServerSendRate / TunnelTargetRate` 优先来自 TECC server 返回的 `X-TECC-Pacing-Rate-Bps` 响应头；普通 video server 不返回该头时该字段为 `-`。

最小推荐运行顺序：

1. `direct`：确认普通 DASH/HTTP3 基线正常。
2. `mdt` 或 `masque`：确认 datagram tunnel baseline 正常。
3. `mst`：确认 stream tunnel 能跑通。
4. `rmdt`：确认 tunnel retransmission 与 debug 指标能记录。
5. `tecc`：先跑 `tecc-lite` 记录 feedback，再接 full TECC server pacing。

## 2. 本轮最小实施顺序

本轮实际执行的顺序如下：

1. 给 GoDASH 增加最小 `transport` 抽象层。
2. 保持 `player.go` 主循环不变，只改 `main.go -> http/urlParsing.go -> transport backend` 这一层。
3. 先保留 direct path，再新增 masque path，使两条路径可切换共存。
4. 先只支持命令行切换，不扩展旧 config 文件格式。
5. 先做到“代码可编译、程序可切换、MASQUE 路径能发起隧道连接尝试”，再做实际链路验证。

## 3. 本轮代码改动

### 3.0 关键修改总览

这次真正对“能否跑通 MASQUE 最小链路”起决定作用的修改，可以压缩成下面 8 条：

1. 新增 `transport` 抽象层，让 `direct` 与 `masque` 共存。
2. 在 `main.go` 增加：
   - `-transport`
   - `-masqueProxyTemplate`
   - `-masqueInsecure`
3. 把 `http/urlParsing.go` 的底层 client 创建逻辑改为委托给 backend。
4. 在 `transport/masque.go` 中手工实现最小版 CONNECT-UDP client backend。
5. 在 `transport/masque.go` 中把内层 QUIC `ConnectionIDLength` 固定为 `0`，解决 CID 冲突。
6. 在 `transport/masque.go` 中为 MASQUE 虚拟 `PacketConn` 定义独立 `LocalAddr`，解决 multiplexer 误复用问题。
7. 把本地客户端依赖的 `quic-go/http3` datagram setting 从旧 draft 的 `0xffd277` 改成 RFC 9297 的 `0x33`。
8. 把本地客户端依赖中的 `MaxDatagramFrameSize` 从 `1200` 提到 `1500`，使外层能承载内层 QUIC Initial 回包。

### 3.1 新增 transport 抽象层

新增目录：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/transport`

新增文件：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/transport/backend.go`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/transport/direct.go`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/transport/masque.go`

设计意图：

- `DirectBackend` 负责保留原有 `HTTP/3` 直连行为。
- `MasqueBackend` 负责最小版 `CONNECT-UDP` 接入。
- `Backend` 接口把 `GetHTTPClient(...)` 和 `ProtocolLabel(...)` 抽出来，避免改动 `player` 和 ABR 主逻辑。

### 3.2 main.go 增加切换入口

修改文件：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/main.go`

新增参数：

- `-transport`
- `-masqueProxyTemplate`
- `-masqueInsecure`

当前支持：

- `-transport direct`
- `-transport masque`

当前策略：

- 第一阶段仅支持命令行切换。
- 还没有把 `transport / masqueProxyTemplate / masqueInsecure` 写入旧的 config 文件体系。

### 3.3 http/urlParsing.go 改为走 backend

修改文件：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/http/urlParsing.go`

本轮改动要点：

- 增加 `activeBackend`。
- 增加 `SetTransportBackend(...)`。
- `GetHTTPClient(...)` 不再自己构造底层 client，而是委托给 backend。
- 下载响应时的 `Protocol` 字段改为通过 `activeBackend.ProtocolLabel(...)` 输出。

这样做的目的很直接：

- `player.go -> http.GetFile(...)` 主调用链不改。
- direct path 与 MASQUE path 在同一个 `http` 层入口切换。

### 3.4 global 增加 transport 相关常量

修改文件：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/global/globalVar.go`

新增常量：

- `TransportName`
- `TransportDirect`
- `TransportMasque`
- `MasqueProxyTemplateName`

### 3.5 最小 MASQUE backend 的实现方式

当前 `MasqueBackend` 不是直接复用 `masque-go-original` 的 client 库，而是基于当前客户端仓库使用的旧版 `quic-go` API 手工实现的最小版接入。

原因：

- 当前 GoDASH 客户端使用的是旧版 `github.com/lucas-clemente/quic-go`。
- `masque-go-original` 使用的是新版 `github.com/quic-go/quic-go`。
- 两者 API 不能直接混用。

所以本轮采用的办法是：

1. 外层到 proxy：
   使用旧版 `http3.RoundTripper` 建立 HTTP/3 连接。
2. CONNECT-UDP 请求：
   用 `RoundTripOpt(... DontCloseRequestStream: true)` 发起 CONNECT 请求，并保留 request stream。
3. HTTP Datagram：
   在旧版 API 下，使用连接级 datagram 接口 `SendMessage / ReceiveMessage` 手工封装。
4. 内层到 media server：
   在自定义 `net.PacketConn` 上，再调用 `quic.DialEarlyContext(...)` 建立“内层 QUIC”。

## 4. 本轮为兼容旧版 quic-go 做的两次修补

在最小运行验证中，先后遇到两个与旧版 `quic-go` 相关的兼容问题，因此做了两次补丁。

### 4.1 第一次修补：连接 ID 长度冲突

错误现象：

```text
cannot use 4 byte connection IDs on a connection that is already using 0 byte connction IDs
```

原因判断：

- 旧版 `quic-go` 在“自定义 `PacketConn` 上再次拨号”时，会对 `ConnectionIDLength` 有默认行为。
- 默认值与当前隧道环境不一致，导致拨号阶段冲突。

处理方式：

- 在 `transport/masque.go` 中，给内层 QUIC 显式设置：

```go
quicConf.ConnectionIDLength = 0
```

### 4.2 第二次修补：内外层 PacketConn 地址冲突

错误根因：

- 旧版 `quic-go` 的 multiplexer 用 `LocalAddr().Network() + LocalAddr().String()` 作为底层 socket 复用键。
- 我们最初把 MASQUE 隧道内部的 `PacketConn` 暴露成了外层真实 UDP 地址。
- 结果内层虚拟连接和外层真实连接被误判成同一个 socket。

处理方式：

- 在 `transport/masque.go` 中给内层隧道 `PacketConn` 定义了独立的地址类型：

```go
type masqueAddr struct{ value string }

func (m masqueAddr) Network() string { return "connect-udp" }
func (m masqueAddr) String() string  { return m.value }
```

- 并把 `LocalAddr()` 改成 MASQUE 虚拟地址，而不是复用外层 UDP 地址。

## 5. 编译级验证

执行位置：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr`

执行结果：

- `go build ./...` 通过。
- `./godash -h` 已能看到：
  - `-transport`
  - `-masqueProxyTemplate`
  - `-masqueInsecure`

说明：

- 代码已具备 direct / masque 两条路径的切换入口。
- 但“可编译”不代表“链路已完全跑通”，因此继续做运行验证。

## 6. 最小运行验证环境

### 6.1 参与组件

media server：

- 项目：`has-quicgo`
- 启动脚本：`/home/hellodaniel0/has-quicgo/scripts/run_server.sh`
- 服务地址：`https://127.0.0.1:4433`

MASQUE proxy：

- 项目：`masque-go-original`
- 二进制：`/home/hellodaniel0/masque-go-original/bin/masque-proxy`
- 监听地址：`127.0.0.1:4443`
- URI Template：

```text
https://127.0.0.1:4443/masque?h={target_host}&p={target_port}
```

GoDASH client：

- 项目：`cross-nossdav-go/cross-layer-implementation/godash-qlogabr`
- 二进制：`./godash`

### 6.2 工具链说明

`masque-go-original` 的 `go.mod` 要求：

```text
go 1.25
```

本机默认 Go 是：

```text
go1.24.0
```

因此编译 proxy 时使用了自动工具链：

```bash
GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go build -o /home/hellodaniel0/masque-go-original/bin/masque-proxy ./cmd/proxy
```

## 7. 最小运行验证步骤

### 7.0 完整运行步骤总览

如果只想按操作顺序重跑，可以直接按下面顺序执行：

1. 启动 `has-quicgo` 的 HTTP/3 media server。
2. 编译并启动 `masque-go-original` 的 proxy。
3. 先跑一轮 direct 基线，确认 server 与客户端直连都正常。
4. 跑第一轮 MASQUE 最小验证。
5. 如果只看到超时，则继续做协议级调试：
   - 修 datagram setting
   - 修外层 datagram MTU
6. 重新跑 MASQUE 最小验证。
7. 检查：
   - 终端输出里的 `Protocol`
   - `files/masque-minimal-test`
   - `metrics_log.txt`
   - `logs/client_*.qlog`

### 7.1 启动 media server

执行目录：

- `/home/hellodaniel0/has-quicgo`

命令：

```bash
./scripts/run_server.sh
```

说明：

- 本次验证时，`h3server` 已经在本机 `*:4433/udp` 运行。

### 7.2 构建并启动 MASQUE proxy

编译命令：

```bash
cd /home/hellodaniel0/masque-go-original
GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go build -o /home/hellodaniel0/masque-go-original/bin/masque-proxy ./cmd/proxy
```

启动命令：

```bash
/home/hellodaniel0/masque-go-original/bin/masque-proxy \
  -b 127.0.0.1:4443 \
  -t 'https://127.0.0.1:4443/masque?h={target_host}&p={target_port}' \
  -c /home/hellodaniel0/has-quicgo/certs/server.crt \
  -k /home/hellodaniel0/has-quicgo/certs/server.key
```

端口检查：

```bash
ss -lunp | rg ':4443'
```

验证时观察到：

```text
UNCONN ... 127.0.0.1:4443 ... users:(("masque-proxy",...))
```

### 7.3 Direct path 对照验证

目的：

- 证明 media server 本身可用。
- 证明当前客户端直连路径可获取 MPD 与前几个 segment。
- 避免把 server 侧问题误判成 MASQUE 问题。

执行目录：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr`

命令：

```bash
./godash \
  -url '[https://127.0.0.1:4433/moi.mpd]' \
  -adapt bba2 \
  -codec h264 \
  -initBuffer 2 \
  -maxBuffer 20 \
  -maxHeight 1080 \
  -streamDuration 4 \
  -storeDASH on \
  -outputFolder masque-direct-baseline \
  -debug on \
  -terminalPrint on \
  -logFile godash_direct_baseline \
  -printHeader '{"Algorithm":"on","Seg_Dur":"on","Codec":"on","Width":"on","Height":"on","FPS":"off","Play_Pos":"off","RTT":"on","Seg_Repl":"on","Protocol":"on","P.1203":"off","Clae":"off","Duanmu":"off","Yin":"off","Yu":"off"}' \
  -QoE off \
  -quic on
```

Direct 对照结果：

- 成功输出 segment 级日志。
- 成功下载到本地文件：
  - `2sec_isoff-live_init.mp4`
  - `2sec_isoff-live_1.m4s`
  - `2sec_isoff-live_2.m4s`
- `metrics_log.txt` 中出现了：
  - `SegmentDownloadStart`
  - `SegmentArrived`
  - `BUFFERLEVEL`

Direct 对照关键输出摘录：

```text
Seg_# ... Codec ... Protocol
1 ... audio/mp4 ... HTTP/3.0
1 ... h264 ... HTTP/3.0
2 ... audio/mp4 ... HTTP/3.0
2 ... h264 ... HTTP/3.0
```

### 7.4 MASQUE path 最小验证

执行目录：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr`

命令：

```bash
./godash \
  -url '[https://127.0.0.1:4433/moi.mpd]' \
  -transport masque \
  -masqueProxyTemplate 'https://127.0.0.1:4443/masque?h={target_host}&p={target_port}' \
  -masqueInsecure=true \
  -adapt bba2 \
  -codec h264 \
  -initBuffer 2 \
  -maxBuffer 20 \
  -maxHeight 1080 \
  -streamDuration 4 \
  -storeDASH on \
  -outputFolder masque-minimal-test \
  -debug on \
  -terminalPrint on \
  -logFile godash_masque_minimal \
  -QoE off \
  -quic on
```

### 7.5 协议级调试后重新验证的最终命令

在完成协议级调试后，最终用于跑通 MASQUE 最小链路的命令是：

```bash
./godash \
  -url '[https://127.0.0.1:4433/moi.mpd]' \
  -transport masque \
  -masqueProxyTemplate 'https://127.0.0.1:4443/masque?h={target_host}&p={target_port}' \
  -masqueInsecure=true \
  -adapt bba2 \
  -codec h264 \
  -initBuffer 2 \
  -maxBuffer 20 \
  -maxHeight 1080 \
  -streamDuration 4 \
  -storeDASH on \
  -outputFolder masque-minimal-test \
  -debug on \
  -terminalPrint on \
  -logFile godash_masque_minimal \
  -printHeader '{"Algorithm":"on","Seg_Dur":"on","Codec":"on","Width":"on","Height":"on","FPS":"off","Play_Pos":"off","RTT":"on","Seg_Repl":"on","Protocol":"on","P.1203":"off","Clae":"off","Duanmu":"off","Yin":"off","Yu":"off"}' \
  -QoE off \
  -quic on
```

## 8. 最小运行验证结果

本节记录“第一次最小验证”的结果。该轮结果用于定位问题，不代表本文档最后的最终状态。

### 8.1 Direct path 结果

结论：

- Direct path 成功。
- 本地 media server 正常。
- GoDASH 直连 `HTTP/3` 拉流逻辑正常。

### 8.2 MASQUE path 结果

结论：

- 第一次 MASQUE path 最小验证未完全跑通。
- 该轮结果不是“完全失败”，而是“已经到达 proxy，并由 proxy 向 media server 发起 UDP 转发，但内层 QUIC/HTTP3 MPD 请求仍然超时”。

GoDASH 端最终错误：

```text
Get "https://127.0.0.1:4433/moi.mpd": timeout: no recent network activity
```

同时可见一条辅助提示：

```text
connection doesn't allow setting of receive buffer size. Not a *net.UDPConn?
```

这条提示来自内层 QUIC 使用的是自定义 `PacketConn`，它本身不是主故障，但说明当前实现仍然是“QUIC over 虚拟 PacketConn”的兼容路径。

### 8.3 Proxy 侧观察到的现象

proxy 前台日志中能看到：

```text
proxying send side to 127.0.0.1:4433 failed: timeout: no recent network activity
proxying receive side to 127.0.0.1:4433 failed: read udp 127.0.0.1:xxxxx->127.0.0.1:4433: use of closed network connection
```

这说明至少有三件事已经成立：

1. GoDASH 已经向 proxy 发起了 CONNECT-UDP。
2. proxy 已经尝试把流量转发到 `127.0.0.1:4433`。
3. 当前卡点发生在“通过隧道转发后的内层 QUIC/HTTP3 建链或 datagram 兼容”阶段，而不是更前面的参数解析或 proxy 启动阶段。

## 9. 当前判断

本节同样对应“第一次最小验证后”的阶段性判断，后续已在第 13 节以后被继续推进。

### 9.1 已经确认可工作的部分

- GoDASH 新增了 `transport` 入口，能够在程序入口切换 `direct` / `masque`。
- 客户端最小 MASQUE backend 已经接入主调用链。
- `masque-go-original` proxy 可在本机启动并监听。
- Direct path 基线验证成功。
- MASQUE path 已经能把请求推进到 proxy 侧，并触发 proxy 到 media server 的 UDP 转发尝试。

### 9.2 仍未跑通的部分

- 通过 MASQUE 隧道完成“内层 QUIC handshake -> HTTP/3 GET MPD -> segment 下载”的完整闭环。
- 因此当前还没有看到 MASQUE 模式下的：
  - MPD 成功解析
  - 第一个 segment 成功到达
  - `metrics_log.txt` 正常增长
  - `Protocol` 输出为 `HTTP/3.0+MASQUE`
  - MASQUE 实验结果目录下的下载文件

## 10. 当前最可能的剩余阻塞点

基于这次最小验证，当前最可能的剩余问题有两个方向：

1. 旧版 `quic-go` 下手工拼接的 HTTP Datagram / QUIC Datagram 语义，与 `masque-go-original` 期望的 datagram 行为仍有偏差。
2. 旧版 `http3.RoundTripper + 自定义 PacketConn + 内层 QUIC` 的组合虽然已能发起尝试，但还没有达到 `masque-go-original` 所需的完全兼容程度。

换句话说：

- 当前问题已经从“架构入口缺失”推进到“协议细节兼容”层面。
- 下一步不应该再重构主框架，而应该集中调试 `transport/masque.go` 的 datagram 映射和内层 QUIC 行为。

## 11. 本轮相关文件与结果路径

### 11.1 本轮修改的客户端代码文件

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/main.go`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/http/urlParsing.go`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/global/globalVar.go`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/transport/backend.go`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/transport/direct.go`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/transport/masque.go`

### 11.2 本轮验证输出文件

Direct 对照输出：

- `/home/hellodaniel0/has-quicgo/results/masque_validation/godash_direct_stdout.log`

MASQUE 验证输出：

- `/home/hellodaniel0/has-quicgo/results/masque_validation/godash_masque_stdout.log`

客户端 debug 日志：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/logs/godash_direct_baseline.txt`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/logs/godash_masque_minimal.txt`

Direct 下载产物：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/files/masque-direct-baseline`

MASQUE 下载产物目录：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/files/masque-minimal-test`

当前状态：

- 在第一次最小验证时，该目录尚无成功下载文件。

## 12. 本轮结论

本节是“第一次最小验证结束时”的阶段性结论，不是本文最终结论。

本轮最小接入的工程目标已经完成一大半：

- “在不改 player 主循环的前提下，把 MASQUE 接到 GoDASH 框架里”这件事已经落地。
- direct / masque 双路径切换入口已经具备。
- 代码已经可编译、可执行、可发起 MASQUE 路径尝试。

但从运行结果看，当前状态应准确表述为：

> MASQUE 最小接入骨架已经完成，proxy 已能接到请求并尝试转发，但 `GoDASH -> MASQUE proxy -> media server` 的内层 QUIC/HTTP3 拉流链路尚未完全打通，当前卡在 datagram / 隧道兼容层。

这意味着下一步应进入：

- `transport/masque.go` 的协议级调试
- datagram 格式核对
- 内层 QUIC handshake 与 HTTP/3 请求行为对齐

而不是再去改 ABR、QoE、player 主状态机。

## 13. 后续协议级调试记录

在完成上面那一轮最小验证后，继续针对以下三点做了协议级调试：

1. `transport/masque.go` 的 datagram 映射实现。
2. 外层 HTTP Datagram 的协商格式。
3. 外层 datagram MTU 是否足以承载“内层 QUIC Initial + MASQUE 包装”。

### 13.1 关键定位一：客户端仍在使用旧版 H3_DATAGRAM setting

调试时发现，客户端本地依赖的旧版 `quic-go/http3` 还在使用旧 draft 的 datagram setting：

- 文件：
  - `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/quic-go/http3/frames.go`
- 原值：

```go
const settingDatagram = 0xffd277
```

而 `masque-go-original` 基于新版 `quic-go`，走的是 RFC 9297：

```go
const settingDatagram = 0x33
```

影响：

- 外层 CONNECT-UDP 虽然可以建立。
- 但 proxy 侧不会按 RFC 9297 认为客户端已经正确协商了 HTTP Datagrams。
- 导致 datagram 路径行为不完整或不一致。

处理：

- 把本地客户端依赖中的 setting 值改为 RFC 9297 的 `0x33`。

最终修改文件：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/quic-go/http3/frames.go`

### 13.2 关键定位二：外层 datagram 尺寸不够，承载不了内层 QUIC Initial

把 proxy 放到前台观测后，拿到了关键错误：

```text
proxying receive side to 127.0.0.1:4433 failed: DATAGRAM frame too large
```

这条日志的含义非常明确：

- proxy 已经从 media server 收到了回包。
- proxy 在尝试把该回包封装成 HTTP Datagram 发回客户端时失败。
- 失败原因不是地址错，也不是 CONNECT-UDP 没建成，而是“外层可发送/接收的 datagram 上限太小”。

根因进一步定位到本地旧版 `quic-go` 的参数：

- 文件：
  - `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/quic-go/internal/protocol/params.go`
- 原值：

```go
const MaxDatagramFrameSize ByteCount = 1200
```

对于 MASQUE 来说，外层 datagram 里承载的是：

- `quarter stream ID`
- `context ID`
- 内层 UDP payload

而内层 UDP payload 又可能是 QUIC Initial 报文，本身就接近 MTU 下限。

因此：

- `1200` 不够
- `1350` 也仍然不够
- 最终将其调到 `1500` 后，本地最小链路才跑通

最终修改文件：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/quic-go/internal/protocol/params.go`

最终值：

```go
const MaxDatagramFrameSize ByteCount = 1500
```

说明：

- 这个值是为了本地最小联通与协议调试先跑通。
- 它不是最终实验环境下的保守默认值。
- 到 Mininet / 仿真 / 更真实链路时，仍需重新评估 MTU、分片、丢包、副作用。

## 14. 协议级调试后的最终最小联通结果

### 14.0 最终成功所依赖的关键修改

最终从“MASQUE 超时”推进到“MASQUE 成功拿到 segment”，依赖的是下面这几组修改同时成立：

应用层接线：

- `main.go` 增加 transport 入口
- `http/urlParsing.go` 改为通过 backend 取 client
- `transport/backend.go` / `transport/direct.go` / `transport/masque.go`

旧版 quic-go 兼容修补：

- `transport/masque.go`
  - `ConnectionIDLength = 0`
  - MASQUE 虚拟 `LocalAddr`
- `quic-go/http3/frames.go`
  - `settingDatagram = 0x33`
- `quic-go/internal/protocol/params.go`
  - `MaxDatagramFrameSize = 1500`

可以把这理解成两层：

1. “框架改造层”负责让 GoDASH 可以选择 MASQUE。
2. “协议兼容层”负责让 MASQUE 真的能把内层 QUIC/HTTP3 流量送过去。

### 14.1 重新执行的 MASQUE 最小验证命令

执行目录：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr`

命令：

```bash
./godash \
  -url '[https://127.0.0.1:4433/moi.mpd]' \
  -transport masque \
  -masqueProxyTemplate 'https://127.0.0.1:4443/masque?h={target_host}&p={target_port}' \
  -masqueInsecure=true \
  -adapt bba2 \
  -codec h264 \
  -initBuffer 2 \
  -maxBuffer 20 \
  -maxHeight 1080 \
  -streamDuration 4 \
  -storeDASH on \
  -outputFolder masque-minimal-test \
  -debug on \
  -terminalPrint on \
  -logFile godash_masque_minimal \
  -printHeader '{"Algorithm":"on","Seg_Dur":"on","Codec":"on","Width":"on","Height":"on","FPS":"off","Play_Pos":"off","RTT":"on","Seg_Repl":"on","Protocol":"on","P.1203":"off","Clae":"off","Duanmu":"off","Yin":"off","Yu":"off"}' \
  -QoE off \
  -quic on
```

### 14.2 最终成功现象

MASQUE 模式下成功输出了 segment 级日志，且 `Protocol` 已明确显示为：

```text
HTTP/3.0+MASQUE
```

关键输出摘录：

```text
Seg_# ... Codec ... Protocol
1 ... audio/mp4 ... HTTP/3.0+MASQUE
1 ... h264 ... HTTP/3.0+MASQUE
2 ... audio/mp4 ... HTTP/3.0+MASQUE
2 ... h264 ... HTTP/3.0+MASQUE
```

这说明：

1. MPD 已成功获取并解析。
2. 第一个 init segment 已成功获取。
3. 至少前两个 media segment 已成功经 MASQUE 隧道下载。
4. `player / http / logging` 主链路在 MASQUE 模式下已经闭环。

这也是本轮最关键的运行结果：

- 终端输出中，`Protocol` 不再是 `HTTP/3.0`，而是 `HTTP/3.0+MASQUE`
- 说明业务请求已经明确走到了 MASQUE backend

### 14.3 下载产物

MASQUE 成功下载后的文件位于：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/files/masque-minimal-test`

当前可见：

- `2sec_isoff-live_init.mp4`
- `2sec_isoff-live_1.m4s`
- `2sec_isoff-live_2.m4s`
- `logDownload.txt`

### 14.4 metrics_log.txt

`metrics_log.txt` 中已经出现：

- `SegmentDownloadStart`
- `SegmentArrived`
- `BUFFERLEVEL`

说明日志主链路在 MASQUE 模式下也已经工作。

### 14.5 qlog

本次 MASQUE 最小联通后，客户端目录下已生成新的 qlog 文件，例如：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/logs/client_411be076f6eba97376c95a8cf5.qlog`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/logs/client_a3fdd60c83bcd5208f8a21eb14e6f5c3a2.qlog`

### 14.6 Proxy 前台观测

在最终成功的这轮里，proxy 不再报 `DATAGRAM frame too large`。

之后出现的：

```text
proxying send side to 127.0.0.1:4433 failed: Application error 0x100 (remote)
```

是在 client 结束、流被主动关闭后的尾部现象，不再是阻塞链路建立的主错误。

## 15. 现阶段最新结论

经过协议级调试后，当前状态已经从：

> MASQUE 接入骨架完成，但内层 QUIC / HTTP3 还未闭环

推进到：

> GoDASH client -> MASQUE proxy -> media server 的最小链路已经打通，客户端已经能够通过 MASQUE 成功获取 MPD、init segment 和前几个 media segment，并产生日志、metrics 与 qlog。

这意味着第一阶段最关键的目标已经成立：

- direct path 与 MASQUE path 可以共存并切换。
- `player` 主循环没有被重写。
- ABR / logging / metrics 主流程仍然保持原框架。
- MASQUE 已经从“框架接线”进入“可做实验”的状态。

## 16. 现阶段仍需注意的事项

虽然最小链路已经打通，但还需要保持工程判断的严谨性。

当前仍需后续处理或确认的点包括：

1. 当前外层 datagram 上限被提高到了 `1500`，这是为了本地最小联通先跑通。
2. 在 Mininet 或更真实链路条件下，仍需重新评估：
   - MTU
   - 分片风险
   - 丢包放大
   - 是否需要更保守的 outer tunnel datagram 策略
3. 还没有把 outer tunnel 指标正式并入 crosslayer accountant。
4. 还没有把 MASQUE 相关字段系统化写入统一实验结果命名与归档体系。
5. 还没有对长时流、多 segment、QoE 演进和 ABR 切换做系统实验。

## 17. 本轮新增修改文件

除前面第 11 节中的 GoDASH 主体改动外，后续协议级调试还修改了本地客户端依赖：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/quic-go/http3/frames.go`
- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/quic-go/internal/protocol/params.go`

这两个修改是本轮把 MASQUE 最小联通真正打通的关键。

## 18. 最终建议的复现实验步骤

如果后续需要在同一台机器上重新复现这次“最小 MASQUE 联通成功”的过程，推荐按下面顺序执行：

1. 确认 `has-quicgo` 的 server 已经在 `127.0.0.1:4433` 运行。
2. 在 `masque-go-original` 下用 Go 1.25 toolchain 编译 proxy。
3. 启动 proxy 到 `127.0.0.1:4443`。
4. 在 GoDASH 目录先跑一次 direct 基线，确认 media server 正常。
5. 再跑第 7.5 节中的 MASQUE 命令。
6. 检查如下结果：
   - 终端输出里 `Protocol = HTTP/3.0+MASQUE`
   - `files/masque-minimal-test` 下有 `init.mp4`、`1.m4s`、`2.m4s`
   - `metrics_log.txt` 中有 `SegmentDownloadStart`、`SegmentArrived`、`BUFFERLEVEL`
   - `logs/client_*.qlog` 有新的 qlog 文件

## 19. 结果查看位置

这次最终成功的最关键结果，可以直接从下面这些路径看：

文档：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/docs/MASQUE_MINIMAL_INTEGRATION_VALIDATION_CN.md`

MASQUE 下载产物：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/files/masque-minimal-test`

metrics：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/logs/metrics_log.txt`

客户端 qlog：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/logs/client_*.qlog`

客户端 debug 日志：

- `/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr/logs/godash_masque_minimal.txt`

服务端与 proxy 的历史验证输出：

- `/home/hellodaniel0/has-quicgo/results/masque_validation`

## 20. 当前状态版分阶段实施计划

原先的“第一阶段 / 第二阶段 / 第三阶段”计划仍然有价值，但在当前状态下不应再按最初版本理解。

现在更准确的状态划分如下。

### 20.1 第一阶段：最小跑通

状态：

- 已基本完成

已经完成的内容：

1. `transport/` 抽象层已加入
2. `DirectBackend` 已实现
3. `MasqueBackend` 已实现
4. `main.go` 已支持 `-transport direct|masque`
5. `http/` 下载逻辑已改为依赖 backend
6. MASQUE path 已经成功：
   - 请求 MPD
   - 请求 init
   - 请求前几个 segment
7. `metrics_log.txt` 仍然生成
8. direct 与 MASQUE 两条路径已经可以区分

第一阶段新增收尾项：

1. 主日志显式增加 `TransportMode`
2. 用短时 direct / MASQUE 再做一轮字段级验证
3. 后续再补一轮 `QoE on` 的 MASQUE 验证

第一阶段收尾执行结果：

- `TransportMode` 显式字段已经加到打印链路
- 终端输出表头已出现：

```text
Protocol    TransportMode
```

- direct 验证中已看到：

```text
HTTP/3.0           direct
```

说明：

- 第一阶段现在不再是“继续开发接入”，而是“已完成 + 少量收尾验证”。

### 20.2 第二阶段：补全 forward / metrics

状态：

- 当前最应该继续推进的主线

原因：

- 现在系统已经“能通”
- 下一步最重要的是“可分析、可复现、可归档”
- 不应在没有 outer 指标与错误分层的前提下直接跳到 tunnel-aware ABR

建议继续做的内容：

1. 外层 tunnel 建立时间写入日志
2. outer qlog 落盘
3. outer / inner 指标分离记录
4. proxy status / tunnel error 记录
5. 更稳定的 session lifecycle 管理

第二阶段验收标准：

1. 每次 MASQUE 实验都能拿到：
   - inner 主日志
   - outer qlog
   - tunnel setup time
2. 出错时能区分：
   - outer tunnel failed
   - inner H3 failed
   - media request failed

### 20.3 第三阶段：为 tunnel-aware ABR 做预留

状态：

- 暂不优先

原因：

- 如果第二阶段还没做完，outer metrics 即使接进 ABR，也很难判断实验结果是否可信

建议后续做的内容：

1. 为 crosslayer 增加 outer metrics channel
2. 定义 inner / outer 统一时间轴
3. 抽象 tunnel-aware metrics provider
4. 设计三组实验：
   - direct ABR
   - MASQUE-unaware ABR
   - MASQUE-aware ABR

第三阶段验收标准：

1. outer tunnel metrics 能稳定进入独立模块
2. 不影响 direct path 结果
3. 可以对同一网络条件做三组可重复对比实验

## 21. 当前继续执行到哪里了

在把计划改成“当前状态版本”之后，本轮已经继续执行了第一阶段收尾里的第一项：

1. 已新增显式日志字段 `TransportMode`
2. 已通过短时 direct 验证确认打印链路中出现：
   - `Protocol`
   - `TransportMode`

当前最合理的下一步，就是按第 20.2 节进入第二阶段起步项，而不是回头重复第一阶段的接入开发。

## 22. 本轮新增：传输层状态日志化

在把实施计划切换为“当前状态版本”之后，本轮继续完成了第二阶段的两个起步项，但仍保持最小侵入：

1. 新增 `TunnelSetupMs`
2. 新增 `TransportError`

设计原则：

- 不改 `player.go` 主状态机
- 不新增独立日志子系统
- 继续复用现有 per-segment 打印链路
- 由 `transport` 层维护状态快照，`http` / `player` 仅读取

本轮代码改动如下：

1. `transport/backend.go`
   - 新增 `StateSnapshot`
   - 新增：
     - `ResetState()`
     - `SetTunnelSetupTime(...)`
     - `SetLastError(...)`
     - `SnapshotState()`
2. `transport/direct.go`
   - direct backend 初始化时把：
     - `TunnelSetupTime = 0`
     - `LastError = ""`
3. `transport/masque.go`
   - 在 `b.trQuic.Dial(...)` 中记录 MASQUE tunnel 建立耗时
   - 对以下错误点写入 `LastError`：
     - 目标 UDP 地址解析失败
     - proxy template 展开失败
     - CONNECT-UDP request 构造失败
     - outer CONNECT-UDP round trip 失败
     - proxy 非 2xx 响应
     - HTTP stream / hijacker / QUIC connection 暴露失败
     - inner QUIC dial 失败
4. `http/urlParsing.go`
   - 新增：
     - `GetTransportSetupTimeMillis()`
     - `GetLastTransportError()`
5. `logging/debug.go`
   - 为 segment 打印链路新增字段：
     - `TunnelSetupMs`
     - `TransportError`
6. `player/player.go`
   - 每个 segment 的 `SegPrintLogInformation` 现在会附带：
     - `TransportMode`
     - `TunnelSetupMs`
     - `TransportError`

本轮 direct 验证命令：

```bash
timeout 20s ./godash \
  -url '[https://127.0.0.1:4433/moi.mpd]' \
  -adapt bba2 \
  -codec h264 \
  -initBuffer 2 \
  -maxBuffer 20 \
  -maxHeight 1080 \
  -streamDuration 2 \
  -storeDASH off \
  -debug off \
  -terminalPrint on \
  -logFile godash_direct_transport_metrics \
  -printHeader '{"Algorithm":"on","Seg_Dur":"on","Codec":"on","Width":"on","Height":"on","FPS":"off","Play_Pos":"off","RTT":"on","Seg_Repl":"on","Protocol":"on","TransportMode":"on","TunnelSetupMs":"on","TransportError":"on","P.1203":"off","Clae":"off","Duanmu":"off","Yin":"off","Yu":"off"}' \
  -QoE off \
  -quic on
```

direct 验证关键输出：

```text
Protocol    TransportMode    TunnelSetupMs       TransportError
HTTP/3.0           direct                0                    -
```

说明：

- 这证明新字段已经进入终端输出与文件日志链路。
- 对 direct path，`TunnelSetupMs=0` 是预期行为。
- `TransportError=-` 表示当前无传输层错误快照。

关于本轮 MASQUE 复测：

- 本轮未完成新的 MASQUE 短测确认。
- 原因不是客户端代码回退，而是本机当时没有可直接复用的 proxy 证书产物，临时启动 `masque-go-original` proxy 时被证书参数阻断。
- 这不影响此前已经完成的 MASQUE 最小跑通记录，但意味着下一步仍应补一次新的 MASQUE 验证，把 `TunnelSetupMs` 在代理路径下也实际打出来。

因此，当前状态应更新为：

1. 第一阶段：已完成
2. 第二阶段：已启动，已落地
   - `TunnelSetupMs`
   - `TransportError`
3. 第二阶段尚未完成：
   - outer qlog 持久化
   - outer / inner 指标分离
   - MASQUE 路径下的新字段复测

## 23. 第二阶段继续执行记录

本轮继续推进了用户指定的两个动作：

1. 先补一次 MASQUE 短测，检查 `TunnelSetupMs` / `TransportError`
2. 再补 outer qlog 与 inner/outer 指标分离

### 23.1 本轮代码改动

新增或补强的点：

1. outer qlog
   - 在 `transport/backend.go` 中新增 `buildOuterQuicConfig()`
   - outer QUIC 连接现在会尝试写入：
     - `logs/masque_outer_<connid>.qlog`
2. inner/outer 协议字段分离
   - `global/globalVar.go` 新增：
     - `InnerProtocol`
     - `OuterProtocol`
   - `logging/debug.go` / `player/player.go` 已接入：
     - `InnerProtocol`
     - `OuterProtocol`
     - `TunnelSetupMs`
     - `TransportError`
3. transport state 扩展
   - `transport/backend.go` 的 `StateSnapshot` 现在包含：
     - `TunnelSetupTime`
     - `OuterProtocol`
     - `LastError`
4. MASQUE transport debug
   - `transport/masque.go` 在以下关键点增加 debug 输出：
     - tunnel setup failed
     - tunnel established
     - inner QUIC dial failed
     - inner QUIC dial established

### 23.2 本轮验证结果

#### A. outer qlog

已经观察到 outer qlog 文件被创建，例如：

- `logs/masque_outer_a64f018e39c3d2543f4d7464aad618.qlog`
- `logs/masque_outer_dff3c486c0784646b8e13dd9b0c74d.qlog`

说明：

- MASQUE 外层 QUIC tracer 已经接入到代码路径中。
- 当前这些文件在被 `timeout` 强制终止时可能为 `0` 字节，因此“outer qlog 已接入”可以确认，但“每次实验都能稳定落完整 outer qlog”还不能完全确认。

#### B. inner / outer 指标分离

已经在主日志结构中新增：

- `Protocol`
- `TransportMode`
- `InnerProtocol`
- `OuterProtocol`
- `TunnelSetupMs`
- `TransportError`

说明：

- 现在 direct / MASQUE 实验已经具备“字段层面的内外分离能力”。
- `Protocol` 继续保留兼容旧输出。
- `InnerProtocol` / `OuterProtocol` 用于后续正式实验分析。

#### C. `TransportError` 验证

使用故意错误的 `-masqueProxyTemplate` 做了快速失败验证：

```bash
./godash \
  -url '[https://127.0.0.1:4433/moi.mpd]' \
  -transport masque \
  -masqueProxyTemplate 'http://%zz' \
  -masqueInsecure=true \
  -adapt bba2 \
  -codec h264 \
  -streamDuration 2 \
  -storeDASH off \
  -debug on \
  -terminalPrint off \
  -logFile godash_masque_stage2_errfmt \
  -QoE off \
  -quic on
```

对应 debug 日志中已经看到：

```text
DEBUG: ... MASQUE tunnel setup failed: parse "http://%zz": invalid URL escape "%zz"
```

这说明：

- `TransportError` 的底层来源已经真实生效
- transport 层错误不再只能混在泛化的下载失败里

#### D. `TunnelSetupMs` 验证现状

本轮使用正常 proxy 进行了 MASQUE 短测，但当前现象是：

- outer qlog 文件会被创建
- 客户端 debug 日志停在：
  - `Get the url https://127.0.0.1:4433/moi.mpd`
- 还没有拿到新的：
  - `MASQUE tunnel established: ... setup_ms=...`

这意味着：

1. 外层 MASQUE 路径已经被触发
2. 但当前这轮环境下，CONNECT-UDP / outer round trip 仍然没有在预期时间内返回
3. 所以 `TunnelSetupMs` 的“成功路径实测值”目前仍待补一轮验证

### 23.3 当前第二阶段完成度判断

可以确认已经完成的部分：

1. outer qlog 代码接入
2. inner / outer 指标字段分离
3. `TransportError` 真实生效验证

还没有完全闭环的部分：

1. `TunnelSetupMs` 在 MASQUE 成功路径下的实测打印
2. outer qlog 的稳定、完整落盘

因此，第二阶段当前状态更准确地说是：

- 已明显推进
- 但还没有达到“全部可验收完成”


## 24. quic-go v0.59 升级后的 MASQUE 接入切换记录（2026-04-22）

### 24.1 本轮目标

本轮不再继续保留此前的“自写最小 MASQUE 接入逻辑”，而是改为：

1. 客户端继续保留 `direct / masque` 两种模式切换入口
2. `direct` 继续保持可编译、可运行
3. MASQUE 客户端侧改为调用 `masque-go-original` 的官方 client 实现
4. 保留当前实验已经接入的观测字段：
   - `TunnelSetupMs`
   - `OuterProtocol`
   - `TransportError`
   - outer qlog

### 24.2 本轮代码调整

1. `quic-go` 升级
   - 客户端侧 `quic-go` 已切到官方 `v0.59.0`
   - `direct` 路径已重新编译并验证通过

2. `transport/masque.go` 切换实现来源
   - 删除此前自写的 CONNECT-UDP / capsule / custom `PacketConn` 最小实现
   - 改为使用 `github.com/quic-go/masque-go` 的 `masque.Client.Dial(...)`
   - inner HTTP/3 client 仍通过自定义 `http3.Transport.Dial` 进入 MASQUE 隧道

3. 依赖接入
   - `go.mod` 增加 `github.com/quic-go/masque-go`
   - 使用本地 `replace` 指向：
     - `/home/hellodaniel0/masque-go-original`

4. 工具链兼容处理
   - 当前客户端构建工具链是 `go1.24.0`
   - 本地 `masque-go-original/go.mod` 原先声明 `go 1.25`
   - 为了在当前环境编译，只将版本声明下调为 `go 1.24`
   - 未修改 `masque-go-original` 的核心实现逻辑

### 24.3 本轮验证结果

#### A. direct 路径

已确认：

1. 客户端仓库可以成功编译
2. localhost direct 短测通过
3. Mininet direct 已通过
4. `topo_test.py` 已切到：
   - `use-go1.24.sh`

因此，`direct` 当前是正确且可继续使用的。

#### B. MASQUE 接入来源

已确认：

1. 当前 `godash` 的 MASQUE 路径已经不再使用此前自写的最小 CONNECT-UDP 实现
2. 当前代码已经切换为调用 `masque-go-original` client
3. outer qlog 仍然会生成，例如：
   - `logs/masque_outer_007a703c123792dccb3b6b.qlog`
   - `logs/masque_outer_68d6f3959f09af131186faa989b2.qlog`

这说明：

- outer QUIC / MASQUE 外层链路已经真实进入运行路径
- 不是停留在“未调用 official client”的状态

#### C. localhost MASQUE 当前现象

在当前 localhost 环境下：

1. `godash` 走 MASQUE 时失败
2. 失败错误为：
   - `quic: transport closed: EOF`
3. 使用最小官方 `masque-go-original` 风格对照程序，在同样的 localhost server / proxy 环境下，结果仍然是：
   - `quic: transport closed: EOF`

这说明当前结论应当修正为：

- 现在的主要问题已经不是“自写最小接入逻辑不兼容”
- 即使切回 `masque-go-original` 官方 client 思路，当前 localhost 环境下仍然存在运行时失败

### 24.4 当前状态判断

截至本节记录时：

1. `direct` 已闭环
2. MASQUE 代码入口已切换到 `masque-go-original` 官方实现路径
3. MASQUE 编译已通过
4. MASQUE 运行态仍未闭环
5. 当前阻塞点是 localhost 环境下的运行时 `EOF`，而不是编译或模式切换入口

### 24.5 下一步排查重点

建议按以下顺序继续排查：

1. 检查 `masque-proxy` 当前监听实例、模板、证书与 server 环境是否完全匹配
2. 用 fresh official proxy + fresh minimal official client 再做一轮 localhost 对照实验
3. 再回到 `godash` 中分析同环境下 inner QUIC 为什么被 `EOF` 关闭

## 25. `DATAGRAM frame too large` 根因与修复记录（2026-04-22）

### 25.1 现象

在 localhost MASQUE 路径下，客户端表面报错为：

- `quic: transport closed: EOF`

该错误同时出现在：

1. `godash` MASQUE 路径
2. 最小官方 `masque-go-original` client / proxy 对照实验

因此可以确认：

- 这不是 `godash` 私有接入层独有问题
- 也不是此前自写最小 MASQUE 实现残留导致的问题

### 25.2 根因定位过程

通过在 `masque-go-original` proxy 侧增加最小日志，确认了以下事实链：

1. proxy 能收到 `CONNECT-UDP` 请求
2. proxy 能把 inner QUIC datagram 发到目标 server
3. target server 也确实会回 UDP 包
4. 失败点出现在 proxy 将 target server 的回包重新封装为 outer HTTP Datagram 时

当时的关键 proxy 日志为：

```text
CONNECT-UDP request target=127.0.0.1:4433
CONNECT-UDP proxy connected target=127.0.0.1:4433 next_hop=127.0.0.1:4433
proxy datagram send target=127.0.0.1:4433 bytes=1280
proxy datagram recv source=127.0.0.1:4433 bytes=1280
proxying receive side to 127.0.0.1:4433 failed: DATAGRAM frame too large
reading from request stream failed: EOF
```

这说明真实根因是：

- target server 返回的 inner QUIC UDP 包大小是 `1280`
- proxy 在 outer MASQUE/HTTP Datagram 路径中无法承载这个大小
- 于是 proxy 侧报：
  - `DATAGRAM frame too large`
- 最终在客户端侧表现成：
  - `quic: transport closed: EOF`

### 25.3 关键约束

本轮同时确认了 `quic-go v0.59` 的一个关键限制：

- `quic.Config.InitialPacketSize` 小于 `1200` 时会被钳到 `1200`

因此：

- 试图把 `InitialPacketSize` 直接设为 `800` 在当前官方 `quic-go v0.59` 上不会真实生效
- 客户端侧能做到的是：
  - 将 inner 初始包压到 `1200`
  - 并关闭 Path MTU Discovery

### 25.4 修复策略

修复采用了“两侧同时约束”的最小方案：

1. `godash` inner QUIC client 侧：
   - `InitialPacketSize = 1200`
   - `DisablePathMTUDiscovery = true`

2. `has-quicgo` target server 侧：
   - `InitialPacketSize = 1200`
   - `DisablePathMTUDiscovery = true`

服务端修改位置：

- `/home/hellodaniel0/has-quicgo/cmd/h3server/main.go`

### 25.5 修复后验证结果

在同步串行实验中：

1. 临时 `h3server` 使用新配置启动
2. instrumented `masque-proxy` 启动
3. `godash` 通过 MASQUE 请求：
   - `https://127.0.0.1:6533/moi.mpd`

结果：

1. 不再出现：
   - `DATAGRAM frame too large`
2. proxy 日志显示 target server 回包已被压到 `1200` 或更小，例如：
   - `1200`
   - `1068`
   - `970`
   - `731`
3. `godash` 已能真实拉流并打印 segment 输出
4. target server 日志显示真实收到了：
   - `/moi.mpd`
   - `/audio/init.mp4`
   - `/v01/init.mp4`
   - `/audio/1.m4s`
   - `/v01/1.m4s`

这说明：

- localhost MASQUE 成功路径已经闭环
- 之前的 `EOF` 确实是由 `DATAGRAM frame too large` 引起
- 当前最小修复是有效的

### 25.6 对后续实验的影响

这次修复意味着：

1. `direct` 路径继续可用
2. localhost MASQUE 成功路径已恢复
3. 后续 Mininet MASQUE 实验也必须使用带这组 QUIC packet size 约束的 server 版本

否则，仍然可能在 outer MASQUE datagram 承载上重新触发同类问题。

## 26. Mininet MASQUE 成功验证与第二阶段收尾记录（2026-04-22）

### 26.1 验证前提

在完成第 25 节中的修复后：

1. `godash` inner QUIC client 侧已启用：
   - `InitialPacketSize = 1200`
   - `DisablePathMTUDiscovery = true`
2. `has-quicgo` server 侧已启用：
   - `InitialPacketSize = 1200`
   - `DisablePathMTUDiscovery = true`
3. 仓库根目录 `h3server` 已重编，`scripts/run_server.sh` 会优先使用该修复后的二进制

### 26.2 Mininet MASQUE 实验命令

实际执行的实验为：

```bash
sudo python3 scripts/topo_test.py \
  --transport masque \
  --bw 5 \
  --delay 20ms \
  --loss 0 \
  --run-server \
  --run-proxy \
  --run-client \
  --adapt bba2 \
  --stream-duration 2 \
  --experiment-name stage2-masque-mininet-sizefix
```

### 26.3 Mininet MASQUE 实验结果

该实验已成功跑通。

客户端关键输出为：

```text
Protocol               HTTP/3.0+MASQUE
TransportMode          masque
InnerProtocol          HTTP/3.0
OuterProtocol          HTTP/3.0 CONNECT-UDP
TunnelSetupMs          185
TransportError         -
```

并且已经真实打印 segment 下载结果，例如：

- audio segment
- video segment

说明：

1. MASQUE outer QUIC / CONNECT-UDP 已真实建立
2. inner HTTP/3 over QUIC 已真实建立
3. `TunnelSetupMs` 在 Mininet MASQUE 成功路径下已有真实非零实测值
4. `TransportError` 在成功路径下保持 `-`
5. `InnerProtocol / OuterProtocol` 分离输出已经生效

### 26.4 结尾日志说明

本轮成功实验末尾仍可见：

```text
reading from request stream failed: H3 error (0x0) (local)
```

当前判断为：

- 这是连接结束阶段的本地关闭日志
- 不影响本轮 MASQUE 成功判定

原因是：

1. segment 已完成下载
2. 主日志字段已完整输出
3. 实验已正常收尾
4. 不再出现此前导致失败的：
   - `DATAGRAM frame too large`
   - `quic: transport closed: EOF`

### 26.5 第二阶段当前完成度判断

截至本节，第二阶段已经具备以下闭环证据：

1. `direct` 模式编译通过并可运行
2. `direct` localhost / Mininet 验证通过
3. `MASQUE` 模式已切换为 `masque-go-original` 路径
4. `MASQUE` localhost 成功路径验证通过
5. `MASQUE` Mininet 成功路径验证通过
6. `TunnelSetupMs` 已在 MASQUE 成功路径下打印真实非零值
7. `TransportError` 已在失败注入路径下验证真实生效
8. `InnerProtocol / OuterProtocol / TransportMode` 字段已完成分离并成功输出
9. outer qlog 已进入真实代码路径并产出文件
10. `topo_test.py` 已具备 `direct / masque` 两模式入口，并已完成实验级调用验证

### 26.6 第二阶段结论

按当前证据，第二阶段可以认为已经闭环。

更准确地说：

- 代码结构已完整
- `direct / MASQUE` 两种模式均可切换
- localhost 与 Mininet 两层验证均已覆盖
- 关键 transport 指标与日志字段均已落地
- 此前阻塞 MASQUE 的 `DATAGRAM frame too large` 问题已完成根因定位与最小修复

因此，第二阶段当前状态可标记为：

- 已完成
- 可以进入后续验证整理或下一阶段工作

## 27. 第二阶段最终摘要

### 27.1 本阶段目标

第二阶段的目标是：

1. 在客户端中保留 `direct / masque` 两种 transport 模式切换能力
2. 完成 `MASQUE` 的最小可运行接入
3. 打通 localhost 与 Mininet 两层实验路径
4. 将以下 transport 观测字段真实接入输出：
   - `TransportMode`
   - `InnerProtocol`
   - `OuterProtocol`
   - `TunnelSetupMs`
   - `TransportError`
5. 为下一阶段工作提供稳定、可验证的实验基础

### 27.2 本阶段最终代码状态

截至第二阶段结束时：

1. 客户端 `quic-go` 已升级到官方 `v0.59.0`
2. `direct` 模式已恢复到可编译、可运行、可实验状态
3. `MASQUE` 路径已切换到 `masque-go-original` 官方 client 实现思路
4. `topo_test.py` 已具备：
   - `direct` 模式入口
   - `masque` 模式入口
5. 服务端 `h3server` 已加入与 MASQUE 兼容的最小 QUIC packet size 约束：
   - `InitialPacketSize = 1200`
   - `DisablePathMTUDiscovery = true`

### 27.3 本阶段关键问题与修复

第二阶段最关键的阻塞问题是：

- localhost MASQUE 路径表面报错：
  - `quic: transport closed: EOF`

最终定位出的真实根因是：

1. target server 返回的 inner QUIC UDP 包大小为 `1280`
2. proxy 在 outer MASQUE / HTTP Datagram 路径中无法承载该大小
3. proxy 报错：
   - `DATAGRAM frame too large`
4. 最终在客户端侧表现为：
   - `quic: transport closed: EOF`

最终采用的最小修复是：

1. `godash` inner QUIC client 侧启用：
   - `InitialPacketSize = 1200`
   - `DisablePathMTUDiscovery = true`
2. `has-quicgo` server 侧启用：
   - `InitialPacketSize = 1200`
   - `DisablePathMTUDiscovery = true`

修复后验证确认：

- 不再出现 `DATAGRAM frame too large`
- localhost MASQUE 成功路径恢复
- Mininet MASQUE 成功路径恢复

### 27.4 本阶段验证结论

#### A. direct

已完成：

1. 编译通过
2. localhost 验证通过
3. Mininet 验证通过
4. ABR 切换验证通过

#### B. MASQUE

已完成：

1. 编译通过
2. localhost 成功路径验证通过
3. Mininet 成功路径验证通过
4. `TunnelSetupMs` 在成功路径下已打印真实非零值
5. `TransportError` 在失败注入路径下已验证真实生效
6. `InnerProtocol / OuterProtocol / TransportMode` 分离输出已生效
7. outer qlog 已进入真实代码路径并产出文件

#### C. 实验脚本层

已完成：

1. `topo_test.py` 支持 `direct`
2. `topo_test.py` 支持 `masque`
3. `topo_test.py` 已完成实验级调用验证

### 27.5 本阶段最终判断

按当前代码与验证证据，第二阶段可以明确标记为：

- 已闭环
- 已完成

可直接引用的结论是：

> 第二阶段已经完成 `direct / MASQUE` 双模式切换、localhost 与 Mininet 双层验证，以及 `TunnelSetupMs / TransportError / InnerProtocol / OuterProtocol` 等关键 transport 指标落地。此前阻塞 MASQUE 的 `DATAGRAM frame too large` 问题已完成根因定位与最小修复，当前代码已可作为后续阶段工作的稳定基线。

## 28. 本地与 Mininet 实验命令索引

### 28.1 本地 localhost 实验

#### A. 启动服务端

在 `has-quicgo` 仓库中执行：

```bash
cd /home/hellodaniel0/has-quicgo
./scripts/run_server.sh
```

#### B. direct 模式

在 `godash-qlogabr` 仓库中执行：

```bash
cd /home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr
/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/use-go1.24.sh ./godash \
  -url "[https://127.0.0.1:4433/moi.mpd]" \
  -transport direct \
  -adapt bba2 \
  -codec h264 \
  -initBuffer 2 \
  -maxBuffer 20 \
  -maxHeight 1080 \
  -streamDuration 2 \
  -storeDASH on \
  -outputFolder stage2-direct-local \
  -debug on \
  -terminalPrint on \
  -logFile stage2-direct-local \
  -quic on
```

#### C. MASQUE 模式

要求：

1. 本地 `masque-proxy` 已启动
2. proxy 模板与监听端口匹配

示例命令：

```bash
cd /home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/godash-qlogabr
/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/use-go1.24.sh ./godash \
  -url "[https://127.0.0.1:4433/moi.mpd]" \
  -transport masque \
  -masqueProxyTemplate "https://127.0.0.1:4443/masque?h={target_host}&p={target_port}" \
  -masqueInsecure=true \
  -adapt bba2 \
  -codec h264 \
  -initBuffer 2 \
  -maxBuffer 20 \
  -maxHeight 1080 \
  -streamDuration 2 \
  -storeDASH on \
  -outputFolder stage2-masque-local \
  -debug on \
  -terminalPrint on \
  -logFile stage2-masque-local \
  -quic on
```

#### D. 启动本地 MASQUE proxy

在 `masque-go-original` 仓库中执行：

```bash
cd /home/hellodaniel0/masque-go-original
/home/hellodaniel0/cross-nossdav-go/cross-layer-implementation/use-go1.24.sh go run ./cmd/proxy \
  -b 127.0.0.1:4443 \
  -c /home/hellodaniel0/has-quicgo/certs/server.crt \
  -k /home/hellodaniel0/has-quicgo/certs/server.key \
  -t 'https://127.0.0.1:4443/masque?h={target_host}&p={target_port}'
```

### 28.2 Mininet 实验

#### A. direct 模式

在 `has-quicgo` 仓库中执行：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/topo_test.py \
  --transport direct \
  --bw 5 \
  --delay 20ms \
  --loss 0 \
  --run-server \
  --run-client \
  --adapt bba2 \
  --stream-duration 2 \
  --experiment-name stage2-direct-mininet
```

#### B. MASQUE 模式

在 `has-quicgo` 仓库中执行：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/topo_test.py \
  --transport masque \
  --bw 5 \
  --delay 20ms \
  --loss 0 \
  --run-server \
  --run-proxy \
  --run-client \
  --adapt bba2 \
  --stream-duration 2 \
  --experiment-name stage2-masque-mininet-sizefix
```

### 28.3 常用参数说明

#### A. transport

- `-transport direct`
  - 直连 server
- `-transport masque`
  - 通过 MASQUE CONNECT-UDP 建立 outer tunnel，再在 tunnel 中跑 inner HTTP/3

#### B. MASQUE 专用参数

- `-masqueProxyTemplate`
  - 指定 CONNECT-UDP 的 proxy URL 模板
- `-masqueInsecure=true`
  - 跳过 proxy 证书校验，适合本地自签名实验环境

#### C. ABR 参数

- `-adapt bba2`
- `-adapt conventional`
- `-adapt arbiter`

第二阶段中已验证：

- `bba2`
- `conventional`

#### D. 常用实验参数

- `-streamDuration 2`
  - 短测，适合快速验证 transport 与日志字段
- `-debug on`
  - 打开调试日志
- `-terminalPrint on`
  - 终端打印详细输出
- `-storeDASH on`
  - 保留下载结果与相关文件
- `--bw 5 --delay 20ms --loss 0`
  - Mininet 基线链路设置

### 28.4 推荐用途

- 若只验证客户端 transport 切换：
  - 使用 `28.1` 的 localhost 命令
- 若验证实验脚本层与归档链路：
  - 使用 `28.2` 的 Mininet 命令
- 若快速确认 MASQUE 成功路径是否仍正常：
  - 优先运行：
    - localhost MASQUE
    - Mininet MASQUE

## 29. 最小实验矩阵 Runner 说明

### 29.1 设计目标

本节只补录本轮新增的最小实验矩阵 runner，不重复前文第二阶段结论。

runner 的设计目标是：

1. 不接旧 Vegvisir 主框架
2. 只借鉴 Vegvisir 的 JSON 配置结构
3. 直接复用 `has-quicgo/scripts/topo_test.py`
4. 支持以下实验维度：
   - `transport`
   - `abr`
   - `bw`
   - `delay`
   - `loss`
   - `iterations`
5. 自动运行并归档结果

### 29.2 新增文件

本轮新增文件：

1. `/home/hellodaniel0/has-quicgo/scripts/run_experiment_matrix.py`
   - 最小实验矩阵 runner

2. `/home/hellodaniel0/has-quicgo/configs/experiment_matrix_minimal.json`
   - 最小示例矩阵配置

### 29.3 配置结构

最小 JSON 配置结构如下：

```json
{
  "settings": {
    "label": "phase2-direct-masque-compare",
    "iterations": 1,
    "stream_duration": 2,
    "max_buffer": 20,
    "init_buffer": 2,
    "output_folder_prefix": "matrix-h3-test",
    "log_file_prefix": "godash_matrix",
    "skip_prechecks": false,
    "keep_cli_on_failure": false
  },
  "transport": ["direct", "masque"],
  "abr": ["bba2", "conventional"],
  "bw": [5],
  "delay": ["20ms"],
  "loss": [0]
}
```

字段含义：

- `settings.label`
  - batch 名称前缀
- `settings.iterations`
  - 每组参数重复次数
- `settings.stream_duration`
  - 每次实验的播放时长
- `settings.max_buffer`
  - goDASH `maxBuffer`
- `settings.init_buffer`
  - goDASH `initBuffer`
- `settings.output_folder_prefix`
  - client 输出目录前缀
- `settings.log_file_prefix`
  - client 日志名前缀
- `transport`
  - `direct` / `masque`
- `abr`
  - ABR 算法列表
- `bw`
  - 带宽列表
- `delay`
  - 时延列表
- `loss`
  - 丢包率列表

### 29.4 运行逻辑

runner 的执行逻辑是：

1. 读取 JSON 配置
2. 展开参数矩阵
3. 为每个组合生成唯一 `experiment_name`
4. 直接调用 `scripts/topo_test.py`
5. `direct` 组合自动调用：
   - `--run-server --run-client`
6. `masque` 组合自动额外调用：
   - `--run-proxy`
7. 将每次实验结果继续归档到：
   - `results/experiments/<experiment_name>/`
8. 同时把 batch 级结果归档到：
   - `results/batch_runs/<batch_name>/`

### 29.5 操作步骤

#### A. 先查看矩阵展开结果

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py --dry-run configs/experiment_matrix_minimal.json
```

用途：

- 只展开矩阵
- 不实际运行实验
- 用于确认参数组合是否正确

#### B. 正式运行最小矩阵

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py configs/experiment_matrix_minimal.json
```

说明：

- 脚本会在需要时自动提权到 `sudo`
- 底层实际调用 `topo_test.py`

#### C. 失败即停

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py --stop-on-failure configs/experiment_matrix_minimal.json
```

用途：

- 任意一组失败后立即停止整个 batch

#### D. 指定 batch 名称

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py \
  --batch-name phase2-compare-smoke \
  configs/experiment_matrix_minimal.json
```

用途：

- 用固定 batch 名称归档
- 便于后续人工查找与对照

### 29.6 结果归档位置

#### A. 单次实验归档

由 `topo_test.py` 自动归档到：

```text
/home/hellodaniel0/has-quicgo/results/experiments/<experiment_name>/
```

通常包含：

- client logs
- qlog
- server logs
- client files

#### B. batch 级归档

由 `run_experiment_matrix.py` 自动归档到：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/<batch_name>/
```

其中包含：

- `batch.log`
- `runs.csv`
- 原始配置副本
- `resolved_config.json`

### 29.7 当前适用范围

当前这个最小 runner 适合：

1. direct vs MASQUE 小规模对照实验
2. 不同 ABR 算法的批量短测
3. 不同网络条件组合下的自动化运行
4. 在正式扩大实验规模前，先验证参数矩阵、归档路径和日志字段是否正确

当前不包含：

- 旧 Vegvisir image / server / shaper 抽象
- 复杂后处理调度
- 大规模分布式实验管理

### 29.8 推荐使用方式

建议先按以下顺序使用：

1. 先执行 `--dry-run` 确认矩阵展开
2. 再跑最小 4 组或 8 组组合
3. 检查：
   - `results/experiments/`
   - `results/batch_runs/`
   - `runs.csv`
   - 关键 transport 字段输出是否完整
4. 确认无误后，再扩大 `bw / delay / loss / iterations` 规模

### 29.9 本轮执行情况

#### A. `--dry-run` 已通过

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py configs/experiment_matrix_minimal.json --dry-run
```

展开结果：

- `total_runs=4`
- `direct + bba2`
- `direct + conventional`
- `masque + bba2`
- `masque + conventional`

这说明当前最小矩阵已按预期展开为：

- `2 transport × 2 abr × 1 bw × 1 delay × 1 loss × 1 iteration`

#### B. 最小 4 组矩阵已实际跑完

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/run_experiment_matrix.py configs/experiment_matrix_minimal.json
```

最新 batch 目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/phase2-direct-masque-compare-20260422-174027
```

`runs.csv` 结果：

- `direct / bba2`: `success`, `exit_code=0`
- `direct / conventional`: `success`, `exit_code=0`
- `masque / bba2`: `success`, `exit_code=0`
- `masque / conventional`: `success`, `exit_code=0`

本轮共 `4/4` 全部成功。

#### C. 对应实验归档目录

本轮自动生成的实验归档目录为：

- `results/experiments/phase2-direct-masque-compare-20260422-174027__direct__bba2__bw5__d20ms__l0__r1`
- `results/experiments/phase2-direct-masque-compare-20260422-174027__direct__conventional__bw5__d20ms__l0__r1`
- `results/experiments/phase2-direct-masque-compare-20260422-174027__masque__bba2__bw5__d20ms__l0__r1`
- `results/experiments/phase2-direct-masque-compare-20260422-174027__masque__conventional__bw5__d20ms__l0__r1`

#### D. 关键 transport 字段检查

已检查每组实验归档下的 `logDownload.txt`，transport 相关字段完整输出。

`direct` 两组结果：

- `TransportMode = direct`
- `InnerProtocol = HTTP/3.0`
- `OuterProtocol = -`
- `TunnelSetupMs = 0`
- `TransportError = -`

`masque` 两组结果：

- `TransportMode = masque`
- `InnerProtocol = HTTP/3.0`
- `OuterProtocol = HTTP/3.0 CONNECT-UDP`
- `TunnelSetupMs = 169~177 ms`
- `TransportError = -`

#### E. 本轮结论

本轮最小矩阵 runner 实测结果表明：

- 本地与 Mininet 下，`direct / masque` 两种模式当前都已可用
- 最小 runner 当前可以正常执行
- QoE 输出仍包含：
  - `P.1203`
  - `Clae`
  - `Duanmu`
  - `Yin`
  - `Yu`
- 当前已经可以进入下一轮更大规模的 direct vs MASQUE 对照实验

### 29.10 8 组稳定性与 delay 扩展验证

#### A. `iterations=2` 的 8 组重复稳定性

配置文件：

- `configs/experiment_matrix_8runs_iterations2.json`

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py configs/experiment_matrix_8runs_iterations2.json --dry-run
sudo python3 scripts/run_experiment_matrix.py configs/experiment_matrix_8runs_iterations2.json
```

矩阵规模：

- `2 transport × 2 abr × 1 bw × 1 delay × 1 loss × 2 iterations = 8`

batch 目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/phase2-direct-masque-compare-8runs-iter2-20260422-174555
```

结果：

- `8/8` 全部成功
- `direct` 两次重复均为：
  - `TransportMode = direct`
  - `TunnelSetupMs = 0`
  - `TransportError = -`
- `masque` 两次重复均为：
  - `TransportMode = masque`
  - `OuterProtocol = HTTP/3.0 CONNECT-UDP`
  - `TunnelSetupMs = 169~188 ms`
  - `TransportError = -`

结论：

- 同一网络条件下重复运行稳定
- 可以继续引入网络条件维度

#### B. `delay=20ms/50ms` 的 8 组网络条件验证

配置文件：

- `configs/experiment_matrix_8runs_delay2.json`

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py configs/experiment_matrix_8runs_delay2.json --dry-run
sudo python3 scripts/run_experiment_matrix.py configs/experiment_matrix_8runs_delay2.json
```

矩阵规模：

- `2 transport × 2 abr × 1 bw × 2 delay × 1 loss × 1 iteration = 8`

第一次运行结果：

- batch 目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/phase2-direct-masque-compare-8runs-delay2-20260422-191041
```

- 结果：`6/8` 成功，`2/8` 失败
- 失败组合：
  - `direct / conventional / bw=5 / delay=50ms / loss=0`
  - `masque / bba2 / bw=5 / delay=50ms / loss=0`

失败原因定位：

- 不是主链路失败
- 是 `topo_test.py` 的 precheck 使用 `curl` 结果做硬判定
- 在 `50ms` 条件下出现：

```text
curl: (8) Header without colon
Prechecks failed. The script will not start the client.
```

#### C. `topo_test.py` precheck 最小修补

修改文件：

- `/home/hellodaniel0/has-quicgo/scripts/topo_test.py`

修补原则：

- 保留 `ping` 检查
- 增加服务端 `UDP 4433` 监听检查
- MASQUE 模式增加代理 `UDP 4443` 监听检查
- `curl` 改为附加信息，不再作为硬失败条件

目的：

- 避免在较高 delay 条件下因为 `curl fallback` 脆弱性误杀实验

#### D. 修补后重跑 `delay2`

重跑命令：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/run_experiment_matrix.py configs/experiment_matrix_8runs_delay2.json
```

重跑 batch 目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/phase2-direct-masque-compare-8runs-delay2-20260422-192638
```

重跑结果：

- `8/8` 全部成功

关键字段结果：

- `direct / 50ms`：
  - `TransportMode = direct`
  - `TunnelSetupMs = 0`
  - `TransportError = -`
- `masque / 50ms`：
  - `TransportMode = masque`
  - `OuterProtocol = HTTP/3.0 CONNECT-UDP`
  - `TunnelSetupMs = 416~418 ms`
  - `TransportError = -`

结论：

- `delay=20ms/50ms` 条件下 direct / MASQUE 都可稳定跑通
- precheck 误判问题已最小修复

### 29.11 `bw` 扩展与小丢包矩阵验证

#### A. 先扩 `bw`，不扩 `loss`

配置文件：

- `configs/experiment_matrix_bw_only.json`

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py configs/experiment_matrix_bw_only.json --dry-run
sudo python3 scripts/run_experiment_matrix.py configs/experiment_matrix_bw_only.json
```

矩阵规模：

- `2 transport × 2 abr × 3 bw × 2 delay × 1 loss × 1 iteration = 24`

batch 目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/phase2-direct-masque-compare-bw-only-20260422-193204
```

结果：

- `24/24` 全部成功

结论：

- 在 `bw = [3, 5, 10]`
- `delay = [20ms, 50ms]`
- `loss = 0`

这些条件下，`direct / masque` 与 `bba2 / conventional` 都已稳定跑通。

#### B. 在稳定 `bw` 基础上加入 `loss = [0, 0.5]`

配置文件：

- `configs/experiment_matrix_bw_loss_small.json`

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py configs/experiment_matrix_bw_loss_small.json --dry-run
sudo python3 scripts/run_experiment_matrix.py configs/experiment_matrix_bw_loss_small.json
```

矩阵规模：

- `2 transport × 2 abr × 3 bw × 2 delay × 2 loss × 1 iteration = 48`

batch 目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/phase2-direct-masque-compare-bw-loss-small-20260422-193643
```

结果：

- `47/48` 成功
- `1/48` 失败

唯一失败组合：

- `masque / bba2 / bw=5 / delay=50ms / loss=0.5`

对应实验目录：

```text
/home/hellodaniel0/has-quicgo/results/experiments/phase2-direct-masque-compare-bw-loss-small-20260422-193643__masque__bba2__bw5__d50ms__l0.5__r1
```

当前结论：

- `bw` 扩展已闭环
- 小丢包矩阵整体可运行，但尚未完全闭环
- 当前已收敛到单一失败组合，后续应优先定位该组合失败原因

#### C. 失败组定位与最小修补

对唯一失败组合进行日志检查后，确认其失败类型为：

- precheck 失败

不是：

- transport error
- 超时
- 内容下载阶段失败

根因：

- 当 `loss=0.5` 时，`ping -c 3` 出现了非零丢包
- 旧 precheck 仍要求 `ping` 必须 `0% packet loss`
- 导致脚本在客户端真正启动前就提前退出

最小修补内容：

- 当 `loss <= 0` 时，仍要求 `ping` 为 `0% packet loss`
- 当 `loss > 0` 时，只要求 `ping` 至少有响应
- `server/proxy` 监听检查保持不变

对应文件：

- `/home/hellodaniel0/has-quicgo/scripts/topo_test.py`

#### D. 单组重跑确认

重跑命令：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/topo_test.py \
  --transport masque \
  --bw 5 \
  --delay 50ms \
  --loss 0.5 \
  --run-server \
  --run-proxy \
  --run-client \
  --adapt bba2 \
  --stream-duration 2 \
  --experiment-name stage2-single-rerun-masque-bba2-bw5-d50-l0.5
```

结果：

- 该单组已通过
- 客户端成功启动并完成下载
- 关键字段正常输出：
  - `TransportMode = masque`
  - `OuterProtocol = HTTP/3.0 CONNECT-UDP`
  - `TunnelSetupMs = 422`
  - `TransportError = -`

这说明原先的失败不是 MASQUE 主链路问题，而是有损链路下 precheck 判定过严。

#### E. 修补后整批重跑

重跑命令：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/run_experiment_matrix.py configs/experiment_matrix_bw_loss_small.json
```

重跑 batch 目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/phase2-direct-masque-compare-bw-loss-small-20260422-195443
```

重跑结果：

- `48/48` 全部成功

最终结论：

- `bw + loss=[0,0.5]` 的 48 组矩阵在修补 precheck 后已全部跑通
- 当前 `bw` 与小丢包实验层面已闭环

### 29.12 当前实验自动化状态总结

截至本轮，最小实验矩阵 runner 的状态为：

已完成：

- 最小 `4` 组矩阵：`4/4` 成功
- `iterations=2` 的 `8` 组重复稳定性：`8/8` 成功
- `delay=20ms/50ms` 的 `8` 组网络条件验证：修补 precheck 后 `8/8` 成功
- `bw` 扩展的 `24` 组矩阵：`24/24` 成功
- `bw + loss=[0,0.5]` 的 `48` 组矩阵：修补 precheck 后 `48/48` 成功

推荐后续顺序：

1. 在当前已闭环矩阵基础上整理结果表和可视化输入
2. 再决定是否继续扩大 `loss` 档位或增加 `iterations`
3. 若继续扩展，优先保持最小改动，不再回退当前 precheck 修补

### 29.13 当前闭环实验结果初步对比

本节基于已导出的汇总 CSV：

- `/home/hellodaniel0/has-quicgo/results/summaries/phase2_closed_loop_per_run.csv`
- `/home/hellodaniel0/has-quicgo/results/summaries/phase2_closed_loop_aggregated.csv`

统计范围：

- `4` 个已闭环 batch
- `88` 条成功运行
- `48` 个条件组

#### A. `direct` vs `masque` 的 `TunnelSetupMs` 对比

整体统计：

- `direct`
  - 运行数：`44`
  - 平均 `TunnelSetupMs`：`0.0 ms`
  - 最小/最大：`0 / 0`
- `masque`
  - 运行数：`44`
  - 平均 `TunnelSetupMs`：`302.2 ms`
  - 最小/最大：`169 / 1125`

按 `delay` 聚合：

- `20ms`
  - `direct`: `0.0 ms`
  - `masque`: `176.2 ms`
- `50ms`
  - `direct`: `0.0 ms`
  - `masque`: `453.5 ms`

按 `delay + loss` 聚合：

- `masque / 20ms / loss=0`: `175.3 ms`
- `masque / 20ms / loss=0.5`: `178.8 ms`
- `masque / 50ms / loss=0`: `418.3 ms`
- `masque / 50ms / loss=0.5`: `535.5 ms`

结论：

- `direct` 没有隧道建立成本，`TunnelSetupMs` 始终为 `0`
- `masque` 存在真实的隧道建立时延
- `masque` 的 `TunnelSetupMs` 随网络 delay 增大而明显上升
- 在 `50ms + loss=0.5` 下，`TunnelSetupMs` 波动更大，最大值达到 `1125 ms`

#### B. `bw / delay / loss` 对视频下载指标的影响

观察指标：

- `video_del_time`
- `video_del_rate`
- `video_rtt`

按 `bw` 聚合：

- `bw=3`
  - `video_del_time`: `545.7 ms`
  - `video_del_rate`: `1818.0`
  - `video_rtt`: `143.1 ms`
- `bw=5`
  - `video_del_time`: `431.3 ms`
  - `video_del_rate`: `2455.7`
  - `video_rtt`: `130.6 ms`
- `bw=10`
  - `video_del_time`: `450.1 ms`
  - `video_del_rate`: `2552.8`
  - `video_rtt`: `146.2 ms`

结论：

- 从 `3 Mbps` 到 `5 Mbps`，视频下载时延明显下降，下载速率明显上升
- 从 `5 Mbps` 到 `10 Mbps` 的收益不再线性增加
- 当前实验是短时 `2s` smoke test，吞吐收益没有完全展开

按 `delay` 聚合：

- `20ms`
  - `video_del_time`: `324.3 ms`
  - `video_del_rate`: `3001.6`
  - `video_rtt`: `84.5 ms`
- `50ms`
  - `video_del_time`: `639.7 ms`
  - `video_del_rate`: `1476.3`
  - `video_rtt`: `202.8 ms`

结论：

- `delay` 是当前影响最明显的网络因素
- 从 `20ms` 到 `50ms` 后：
  - `video_del_time` 约翻倍
  - `video_del_rate` 明显下降
  - `video_rtt` 明显上升

按 `loss` 聚合：

- `loss=0`
  - `video_del_time`: `443.3 ms`
  - `video_del_rate`: `2406.8`
  - `video_rtt`: `135.2 ms`
- `loss=0.5`
  - `video_del_time`: `532.5 ms`
  - `video_del_rate`: `2045.3`
  - `video_rtt`: `146.5 ms`

结论：

- `0.5%` 的轻微丢包已经带来可见影响
- 表现为：
  - `video_del_time` 增大
  - `video_del_rate` 降低
  - `video_rtt` 略有上升
- 该影响小于 `delay` 变化带来的影响，但趋势稳定存在

#### C. `bba2` vs `conventional` 的 QoE 对比

当前 QoE 指标：

- `P.1203`
- `Clae`
- `Duanmu`
- `Yin`
- `Yu`

`bba2`，`44` 条成功运行：

- `P.1203 = 0.000`
- `Clae = 0.111`
- `Duanmu = 53.056`
- `Yin = -5477.728`
- `Yu = 0.522`

`conventional`，`44` 条成功运行：

- `P.1203 = 0.000`
- `Clae = 0.111`
- `Duanmu = 53.056`
- `Yin = -5477.728`
- `Yu = 0.522`

结论：

- 当前短时实验中，`bba2` 与 `conventional` 的 QoE 指标没有拉开差异
- 主要原因是：
  - `streamDuration=2`
  - 每轮只覆盖非常短的下载过程
  - 多数日志只包含第一段 audio 与第一段 video
  - ABR 没有充分经历多轮 buffer 与码率选择变化
- 因此当前结果更适合证明 transport 与网络条件影响
- 不适合直接作为 ABR QoE 优劣排序结论

后续若要比较 ABR QoE，应优先：

1. 增大 `streamDuration`
2. 增加 `iterations`
3. 让客户端经历更多 segment 下载与码率切换

### 29.14 最小绘图脚本与核心图输出

#### A. 绘图脚本

新增脚本：

```text
/home/hellodaniel0/has-quicgo/scripts/plot_matrix_summary.py
```

默认输入：

```text
/home/hellodaniel0/has-quicgo/results/summaries/phase2_closed_loop_per_run.csv
/home/hellodaniel0/has-quicgo/results/summaries/phase2_closed_loop_aggregated.csv
```

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/plot_matrix_summary.py
```

输出目录：

```text
/home/hellodaniel0/has-quicgo/results/visualizations/
```

#### B. 已生成核心图

当前已生成以下图片：

- `01_tunnel_setup_by_delay.png`
  - `direct` vs `MASQUE` 的 `TunnelSetupMs`
  - 按 `delay=20ms/50ms` 对比
- `02_network_effect_video_del_time.png`
  - `bw / delay / loss` 对 `video_del_time` 的影响
- `03_network_effect_video_del_rate.png`
  - `bw / delay / loss` 对 `video_del_rate` 的影响
- `04_network_effect_video_rtt.png`
  - `bw / delay / loss` 对 `video_rtt` 的影响
- `05_qoe_by_abr.png`
  - `bba2` vs `conventional` 的 QoE 对比
  - 包含 `P.1203 / Clae / Duanmu / Yin / Yu`
- `06_qlog_coverage.png`
  - `direct / MASQUE` 下 inner / outer qlog 覆盖率
- `07_transport_error_rate.png`
  - `direct / MASQUE` 下 `TransportError` 出现率

#### C. 图 01 的检查结论

最初观察到 `01_tunnel_setup_by_delay.png` 中 `direct` 看起来没有柱子。

检查后确认：

- 不是实验数据缺失
- 也不是 CSV 解析错误
- 原因是 `direct` 的 `TunnelSetupMs` 真实值全部为 `0`
- `0` 高度柱状图在默认绘图中不可见

对应数据：

- `direct / 20ms`
  - 样本数：`24`
  - 平均 `TunnelSetupMs`: `0.0 ms`
- `direct / 50ms`
  - 样本数：`20`
  - 平均 `TunnelSetupMs`: `0.0 ms`
- `masque / 20ms`
  - 样本数：`24`
  - 平均 `TunnelSetupMs`: `176.2 ms`
- `masque / 50ms`
  - 样本数：`20`
  - 平均 `TunnelSetupMs`: `453.5 ms`

#### D. 绘图脚本修正

已对绘图脚本做最小修正：

- 对柱状图增加数值标注
- 对 `0` 值显式标注 `0`
- 对 QoE 图改为每个指标一个子图，避免 `Yin` 的大负值压扁其他指标

修正后重新生成所有图片。

#### E. 当前绘图结论

当前图片可以支持以下结论：

1. `direct` 的 `TunnelSetupMs` 始终为 `0`
2. `MASQUE` 存在真实隧道建立时延，且随 `delay` 增大明显上升
3. `delay` 对 `video_del_time / video_del_rate / video_rtt` 的影响最明显
4. `loss=0.5` 会带来可见但相对温和的性能退化
5. 当前短时实验下，`bba2` 与 `conventional` 的 QoE 没有明显差异
6. `MASQUE` 下 outer qlog 覆盖率为 `100%`
7. 成功实验中 `TransportError` 出现率为 `0%`

#### F. 注意事项

当前图表基于短时 `streamDuration=2` 的实验结果。

因此：

- 适合展示 transport 与网络条件影响
- 适合验证 qlog 与指标链路是否完整
- 不适合直接作为 ABR QoE 优劣的最终结论

### 29.15 正式实验准备与 120s pilot 检查

#### A. 新增正式实验配置

为进入更正式的 direct vs MASQUE QoE 对照实验，新增以下配置文件：

- `/home/hellodaniel0/has-quicgo/configs/formal_matrix_pilot_120s.json`
- `/home/hellodaniel0/has-quicgo/configs/formal_matrix_baseline_120s.json`
- `/home/hellodaniel0/has-quicgo/configs/formal_matrix_loss_120s.json`

配置目标：

- `formal_matrix_pilot_120s.json`
  - 6 组 pilot
  - `transport = [direct, masque]`
  - `abr = [conventional, bba2, elastic]`
  - `bw = [5]`
  - `delay = [30ms]`
  - `loss = [0]`
  - `streamDuration = 120`
- `formal_matrix_baseline_120s.json`
  - 162 组正式 baseline
  - `transport = [direct, masque]`
  - `abr = [conventional, bba2, elastic]`
  - `bw = [3, 5, 10]`
  - `delay = [10ms, 30ms, 60ms]`
  - `loss = [0]`
  - `iterations = 3`
  - `streamDuration = 120`
- `formal_matrix_loss_120s.json`
  - 486 组 loss 扩展矩阵
  - 在 baseline 基础上加入 `loss = [0, 1, 2]`

#### B. dry-run 检查结果

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py configs/formal_matrix_pilot_120s.json --dry-run
python3 scripts/run_experiment_matrix.py configs/formal_matrix_baseline_120s.json --dry-run
python3 scripts/run_experiment_matrix.py configs/formal_matrix_loss_120s.json --dry-run
```

结果：

- pilot 矩阵：`6` 组
- baseline 矩阵：`162` 组
- loss 扩展矩阵：`486` 组

dry-run 展开正常。

#### C. 当前内容时长检查

检查当前 has-quicgo 可用 MPD：

```text
/home/hellodaniel0/has-quicgo/assets/public/moi.mpd
/home/hellodaniel0/has-quicgo/assets/public/test.mpd
```

结果：

- `moi.mpd`: `PT12S`
- `test.mpd`: `PT0H0M12.000S`

结论：

- 当前可用内容最大播放时长约为 `12s`
- 这与正式实验计划中的 `streamDuration=120/180` 不匹配

#### D. 120s pilot 执行结果

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/run_experiment_matrix.py configs/formal_matrix_pilot_120s.json
```

batch 目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/formal-pilot-direct-masque-qoe-120s-20260423-142621
```

runner 记录：

- `6/6` 显示为 success
- `direct / masque` 均启动
- `conventional / bba2 / elastic` 均可被命令行接受

但进一步检查 `batch.log` 后发现，goDASH 输出：

```text
*** -streamDuration, 120 seconds, must not be larger than the maximum MPD stream duration of 12 second ***
```

对应结果：

- pilot 没有生成有效的 `logDownload.txt`
- `formal_pilot_120s_per_run.csv` 只有 `runs.csv` 元数据
- 无法提取 `video_tunnelsetupms / QoE / segment completion` 等正式指标
- 因此不能基于该 pilot 生成有效 QoE 对比图

#### E. 当前结论

当前正式 120s/180s 实验暂不能直接启动大矩阵。

原因：

- 实验系统和 direct / MASQUE transport 本身可用
- 但当前 MPD 内容只有约 `12s`
- `streamDuration=120/180` 与内容时长不匹配
- runner 当前只看进程退出码，未识别 goDASH 的参数级失败提示

因此：

- 不应继续运行 `162` 组 baseline 或 `486` 组 loss 矩阵
- 不应把该 120s pilot 结果作为正式 QoE 数据
- 需要先准备足够长的 MPD / content，或临时把正式实验时长调整到不超过 `12s`

#### F. 后续建议

进入正式实验前应先完成以下准备：

1. 准备至少 `120s` 的 DASH 内容与 MPD
2. 确认服务端可通过 direct / MASQUE 下载完整长内容
3. 用单组 direct / MASQUE 各跑一次 `streamDuration=120`
4. 确认生成有效：
   - `logDownload.txt`
   - inner qlog
   - MASQUE outer qlog
   - QoE 字段
5. 再运行 `formal_matrix_baseline_120s.json`

若暂时没有长内容，可先继续使用当前 `12s` 内容做短测矩阵，但结论只能用于 transport pipeline 与短内容 baseline，不能作为正式长时 QoE 结果。

### 29.16 当前 12s 内容下的 direct / MASQUE 正式前置实验

#### A. 背景

由于当前服务端可用 MPD 内容最大时长约为 `12s`，`streamDuration=120/180` 的正式长时实验暂时无法产生有效 QoE 数据。因此先基于当前内容执行 `12s` 实验组，用于验证正式矩阵、runner、归档、统计和绘图流程。

该批次结论只代表当前 `12s` 内容下的短内容 baseline，不等价于后续 `120s/180s` 正式长时 QoE 结论。

#### B. 新增 12s 实验配置

新增配置文件：

```text
/home/hellodaniel0/has-quicgo/configs/formal_matrix_pilot_12s.json
/home/hellodaniel0/has-quicgo/configs/formal_matrix_baseline_12s.json
/home/hellodaniel0/has-quicgo/configs/formal_matrix_loss_12s.json
```

配置含义：

- `formal_matrix_pilot_12s.json`: `6` 组，`direct / masque × conventional / bba2 / elastic × bw=5 × delay=30ms × loss=0 × iterations=1`
- `formal_matrix_baseline_12s.json`: `162` 组，`direct / masque × conventional / bba2 / elastic × bw=3/5/10 × delay=10/30/60ms × loss=0 × iterations=3`
- `formal_matrix_loss_12s.json`: `486` 组，在 baseline 基础上加入 `loss=0/1/2`，当前仅完成 dry-run，尚未执行整批 loss 矩阵

#### C. dry-run 检查

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/run_experiment_matrix.py configs/formal_matrix_pilot_12s.json --dry-run
python3 scripts/run_experiment_matrix.py configs/formal_matrix_baseline_12s.json --dry-run
python3 scripts/run_experiment_matrix.py configs/formal_matrix_loss_12s.json --dry-run
```

结果：

- pilot: `total_runs=6`
- baseline: `total_runs=162`
- loss: `total_runs=486`

结论：12s 矩阵展开正确，runner 可正确组合 `transport / ABR / bw / delay / loss / iterations`。

#### D. 12s pilot 执行结果

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/run_experiment_matrix.py configs/formal_matrix_pilot_12s.json --stop-on-failure
```

批次目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/formal-current12-pilot-direct-masque-qoe-20260423-144317
```

结果：

- `6/6` 成功
- direct / MASQUE 均成功
- `conventional / bba2 / elastic` 均成功
- 可正常生成 `logDownload.txt`
- MASQUE 日志中出现有效字段：`TransportMode=masque`、`InnerProtocol=HTTP/3.0`、`OuterProtocol=HTTP/3.0 CONNECT-UDP`、`TunnelSetupMs>0`、`TransportError=-`

pilot 汇总输出：

```text
/home/hellodaniel0/has-quicgo/results/summaries/formal_current12_pilot_per_run.csv
/home/hellodaniel0/has-quicgo/results/summaries/formal_current12_pilot_aggregated.csv
/home/hellodaniel0/has-quicgo/results/visualizations/formal_current12_pilot/
```

#### E. 12s baseline 执行结果

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/run_experiment_matrix.py configs/formal_matrix_baseline_12s.json --stop-on-failure
```

批次目录：

```text
/home/hellodaniel0/has-quicgo/results/batch_runs/formal-current12-baseline-direct-masque-qoe-20260423-144519
```

结果：

- `162/162` 成功
- direct / MASQUE 均成功
- `conventional / bba2 / elastic` 均成功
- `bw = 3 / 5 / 10 Mbps` 均成功
- `delay = 10 / 30 / 60 ms` 均成功
- `loss = 0`
- 每组均完成归档

baseline 汇总输出：

```text
/home/hellodaniel0/has-quicgo/results/summaries/formal_current12_baseline_per_run.csv
/home/hellodaniel0/has-quicgo/results/summaries/formal_current12_baseline_aggregated.csv
/home/hellodaniel0/has-quicgo/results/visualizations/formal_current12_baseline/
```

#### F. 绘图口径修正

发现问题：原 `05_qoe_by_abr.png` 只按 ABR 汇总 QoE，没有把 `transport=direct/masque` 作为对照维度，因此不能回答“有无 QUIC 隧道对 QoE 的影响”。

最小修正：

- 修改 `/home/hellodaniel0/has-quicgo/scripts/plot_matrix_summary.py`
- `TunnelSetupMs` 图不再硬编码 `20ms / 50ms`
- QoE 图改为 `ABR × transport` 分组柱状图
- 新的 `05_qoe_by_abr.png` 已可直接比较 direct 与 MASQUE 的 QoE 表现

重新生成图片命令：

```bash
cd /home/hellodaniel0/has-quicgo
python3 scripts/plot_matrix_summary.py \
  --per-run results/summaries/formal_current12_baseline_per_run.csv \
  --aggregated results/summaries/formal_current12_baseline_aggregated.csv \
  --output-dir results/visualizations/formal_current12_baseline
```

核心图片：

```text
results/visualizations/formal_current12_baseline/01_tunnel_setup_by_delay.png
results/visualizations/formal_current12_baseline/02_network_effect_video_del_time.png
results/visualizations/formal_current12_baseline/03_network_effect_video_del_rate.png
results/visualizations/formal_current12_baseline/04_network_effect_video_rtt.png
results/visualizations/formal_current12_baseline/05_qoe_by_abr.png
results/visualizations/formal_current12_baseline/06_qlog_coverage.png
results/visualizations/formal_current12_baseline/07_transport_error_rate.png
```

其中：

- `01_tunnel_setup_by_delay.png`: 比较 direct / MASQUE 的 TunnelSetupMs
- `05_qoe_by_abr.png`: 比较 direct / MASQUE 在不同 ABR 下的 QoE
- `06_qlog_coverage.png`: 检查 inner / outer qlog 覆盖
- `07_transport_error_rate.png`: 检查 TransportError 是否出现

#### G. 12s baseline 关键统计结果

基于：

```text
/home/hellodaniel0/has-quicgo/results/summaries/formal_current12_baseline_per_run.csv
```

样本数：

- 成功 runs: `162`
- direct: `81`
- MASQUE: `81`

QoE 按 transport 汇总：

```text
transport   P.1203   Clae    Duanmu    Yin       Yu
direct      0.000    0.241   49.550    3637.035  0.659
masque      0.000    0.241   49.535    3624.535  0.657
```

QoE 按 `ABR × transport` 汇总：

```text
ABR           transport   P.1203   Clae    Duanmu    Yin       Yu
bba2          direct      0.000    0.246   48.456    3133.632  0.522
bba2          masque      0.000    0.246   48.456    3133.632  0.522
conventional  direct      0.000    0.230   51.737    4643.840  0.934
conventional  masque      0.000    0.230   51.694    4606.340  0.928
elastic       direct      0.000    0.246   48.456    3133.632  0.522
elastic       masque      0.000    0.246   48.456    3133.632  0.522
```

TunnelSetupMs 按 `transport × delay` 汇总：

```text
transport   delay   avg TunnelSetupMs
direct      10ms    0.0
direct      30ms    0.0
direct      60ms    0.0
masque      10ms    95.9
masque      30ms    257.6
masque      60ms    501.9
```

网络指标按 transport 汇总：

```text
transport   video_del_time   video_del_rate   video_rtt
direct      452.69           3589.93          135.69
masque      460.60           3522.94          136.99
```

qlog 覆盖：

```text
transport   inner qlog   outer qlog
direct      100%         0%
masque      100%         100%
```

TransportError：

```text
direct:  -
masque:  -
```

#### H. 当前结论

当前 `12s / loss=0` baseline 下：

1. direct 与 MASQUE 都可以稳定完成实验矩阵
2. MASQUE 的主要额外开销体现在 `TunnelSetupMs`
3. `TunnelSetupMs` 随 delay 明显增加
4. 当前短内容实验中，direct 与 MASQUE 的 QoE 差异很小
5. `conventional` 在当前 12s 数据中 QoE 指标略高于 `bba2 / elastic`
6. `bba2` 与 `elastic` 在当前输出上表现几乎一致
7. MASQUE 下 outer qlog 稳定产出
8. 当前成功 runs 中未观察到 TransportError

限制：

- 当前结果是 `12s` 短内容结果
- `loss=1/2` 的 12s loss 矩阵尚未执行
- 该结果不能替代未来 `120s/180s` 长内容正式 QoE 实验

#### I. 下一步建议

建议下一步按以下顺序继续：

1. 先检查 `formal_current12_baseline` 图片是否满足论文图表口径
2. 再执行较小 loss smoke，而不是直接跑完整 `486` 组 loss 矩阵
3. loss smoke 稳定后，再决定是否执行完整 `formal_matrix_loss_12s.json`
4. 后续准备 `120s+` 内容后，再迁移同一矩阵结构到正式长内容实验

## 30. QoE对比实验说明
### 30.11 丢包扫
## 丢包扫 

transport=direct/masque，abr=conventional/bba2/elastic，bw=5，delay=30ms，
     loss=[0,0.1,0.2,0.3,0.5,0.7,1.0]，iterations=5，stream_duration=120（共 210 组）。

  cd /home/hellodaniel0/has-quicgo

**1) 清理 Mininet 残留**

  sudo mn -c
  sudo pkill -f "mininet|mnexec|topo_test.py|run_experiment_matrix.py" || true

**2) 保险起见，把这对残留接口删掉（存在才删）**

  sudo ip link del h2-eth0 2>/dev/null || true
  sudo ip link del s1-eth2 2>/dev/null || true

**3) 重新跑丢包扫**

  sudo python3 scripts/run_experiment_matrix.py configs/formal_output_c_loss_sweep_120s.json --stop-on-failure

### 30.11.1 Pensieve 丢包扫

这组和上面的 `conventional / bba2 / elastic` 不同，`pensieve` 依赖本机先启动官方 `rl_server` 推理服务。

配置文件：

- `configs/formal_output_c_loss_sweep_120s_pensieve.json`

矩阵含义：

- transport=`direct/masque`
- abr=`pensieve`
- bw=`5`
- delay=`30ms`
- loss=`[0,0.1,0.2,0.3,0.5,0.7,1.0]`
- iterations=`5`
- stream_duration=`120`
- 总组数：`2 x 1 x 1 x 1 x 7 x 5 = 70 runs`

#### A. Pensieve 服务启动前置

Pensieve 服务目录：

- `/home/hellodaniel0/cross-nossdav-go/paper-utilities/pensieve/rl_server`

本机使用的 Python 解释器：

- `/home/hellodaniel0/.local/miniforge3-pensieve/envs/pensieve-py38/bin/python`

如果还没装过依赖，先参考前面记录完成 `Miniforge + pensieve-py38` 安装。当前这轮实验实际使用的是：

- `tensorflow==2.7.0`
- `tflearn==0.5.0`
- `protobuf<3.21`
- `Pillow<10`

#### B. 启动 Pensieve 推理服务

执行命令：

```bash
cd /home/hellodaniel0/cross-nossdav-go/paper-utilities/pensieve/rl_server
rm -f /tmp/pensieve.log /tmp/pensieve.pid
(nohup /home/hellodaniel0/.local/miniforge3-pensieve/envs/pensieve-py38/bin/python rl_server_no_training.py </dev/null > /tmp/pensieve.log 2>&1 & echo $! > /tmp/pensieve.pid)
sleep 8
cat /tmp/pensieve.pid
```

说明：

- 服务默认监听 `127.0.0.1:8333`
- 日志写到 `/tmp/pensieve.log`
- PID 写到 `/tmp/pensieve.pid`

#### C. 探活

启动后必须先探活，确认 `rl_server` 可以返回 `0` 到 `5` 之间的动作编号。

探活命令：

```bash
curl -sS -X POST http://127.0.0.1:8333 \
  -H 'Content-Type: application/json' \
  -d '{"lastquality":0,"buffer":4.0,"RebufferTime":0.0,"lastChunkStartTime":0.0,"lastChunkFinishTime":800.0,"lastChunkSize":500000.0,"lastRequest":1}'
```

判定标准：

- 返回 `0`、`1`、`2`、`3`、`4`、`5` 中任意一个数字即可

如需连续探两次，可直接重复执行一次，或查看日志：

```bash
tail -n 40 /tmp/pensieve.log
```

#### D. 跑 Pensieve 丢包扫前的清理

执行目录：

```bash
cd /home/hellodaniel0/has-quicgo
```

清理命令：

```bash
sudo mn -c
sudo pkill -f "mininet|mnexec|topo_test.py|run_experiment_matrix.py" || true
sudo ip link del h2-eth0 2>/dev/null || true
sudo ip link del s1-eth2 2>/dev/null || true
```

#### E. 正式执行 Pensieve 丢包扫

执行命令：

```bash
cd /home/hellodaniel0/has-quicgo
sudo python3 scripts/run_experiment_matrix.py configs/formal_output_c_loss_sweep_120s_pensieve.json --stop-on-failure
//跨层丢包扫
sudo python3 scripts/run_experiment_matrix.py configs/formal_output_c_loss_sweep_120s_crosslayer.json --stop-on-failure
```
补充说明：

- `godash` 默认会把 Pensieve 服务地址当成 `http://127.0.0.1:8333`
- 当前 `topo_test.py` / matrix runner 没有额外覆盖 `-pensieveServer`，因此本机服务必须先在 `127.0.0.1:8333` 正常起来
- 如果服务没起来，客户端侧通常会出现 `pensieve service request failed` 或 reset/predict 失败

## 时延扫  

配置文件 configs/formal_output_c_delay_sweep_120s.json

1. 固定项

  - transport: ["direct", "masque"]
  - abr: ["conventional", "bba2", "elastic"]
  - bw: [5] Mbps
  - loss: [0]
  - stream_duration: 120
  - iterations: 5

    2. 时延档位（建议）

  - delay: ["10ms", "20ms", "30ms", "40ms", "60ms", "80ms", "100ms"]

    3. 总组数

  - 2 × 3 × 7 × 5 = 210 runs

  cd /home/hellodaniel0/has-quicgo
  sudo python3 scripts/run_experiment_matrix.py configs/
  formal_output_c_delay_sweep_120s.json --stop-on-failure
