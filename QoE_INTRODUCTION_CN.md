下面是一份按**仓库当前实现**整理的正式说明文档，描述该仓库 `cross-layer-implementation/godash-qlogabr/qoe/` 中的 QoE 指标及其含义、定义、适用范围与所反映的体验维度，并记录 `-QoE on` 所需的本机 P.1203 环境配置方法。

该仓库在 README 中明确说明，客户端支持输出五类 QoE 模型结果：`P.1203`、`Yu`、`Yin`、`Claye`、`Duanmu`。对应源码位于 `qoe/` 目录中的 `p1203.go`、`yu.go`、`yin.go`、`claye.go`、`duanmu.go`，统一由 `CreateQoE` 调度计算并写入日志结构。([GitHub][1])

---

## 1. 从零配置 `-QoE on`

GoDASH 客户端启动时如果使用：

```bash
-QoE on
```

会先检查系统里是否能找到 `p1203-standalone`。随后在计算 P.1203 时还会调用：

```bash
python3 -m itu_p1203
```

因此，必须让当前终端的 `PATH` 同时满足两件事：

- `p1203-standalone` 能被找到
- `python3` 是安装了 `itu_p1203` 模块的 Python

如果没有配置好，常见报错是：

```text
p1203-standalone has not been found in $PATH, either turn "QoE off" or make sure P1203 has been installed and added to your $PATH
```

或者：

```text
/usr/bin/python3: No module named itu_p1203
```

### 1.1 本仓库推荐的本机安装位置

为了不污染系统 Python，当前仓库推荐把 QoE 工具放在 `.tools/` 下：

```text
.tools/miniforge3-pensieve-local/   # 已安装的 Miniforge/conda
.tools/qoe-py310/                   # QoE 专用 Python 3.10 环境
.tools/itu-p1203/                   # 官方 itu-p1203 源码
```

`.tools/` 已加入 `.gitignore`，不会提交到 Git。

### 1.2 如果已经有 Miniforge

如果已经按 Pensieve 文档安装过：

```text
.tools/miniforge3-pensieve-local/
```

直接创建 QoE 专用 Python 3.10 环境：

```bash
cd /home/quic2/masque-HAS/cross-nossdav-go
mkdir -p /tmp/pensieve-home

HOME=/tmp/pensieve-home \
/home/quic2/masque-HAS/cross-nossdav-go/.tools/miniforge3-pensieve-local/bin/conda \
  create -y -p /home/quic2/masque-HAS/cross-nossdav-go/.tools/qoe-py310 \
  python=3.10
```

### 1.3 如果还没有 Miniforge

先安装 Miniforge 到仓库内：

```bash
cd /home/quic2/masque-HAS/cross-nossdav-go

curl -L -o /tmp/Miniforge3-pensieve.sh \
  https://github.com/conda-forge/miniforge/releases/latest/download/Miniforge3-Linux-x86_64.sh

mkdir -p /tmp/pensieve-home

HOME=/tmp/pensieve-home \
bash /tmp/Miniforge3-pensieve.sh -b -p \
  /home/quic2/masque-HAS/cross-nossdav-go/.tools/miniforge3-pensieve-local
```

然后执行上一节的 `conda create` 命令创建 `.tools/qoe-py310`。

### 1.4 安装 ITU P.1203

官方新版 `itu-p1203` 不在 PyPI 上直接发布为 `itu-p1203` 包，推荐从 GitHub 源码安装：

```bash
cd /home/quic2/masque-HAS/cross-nossdav-go

git clone https://github.com/itu-p1203/itu-p1203.git \
  /home/quic2/masque-HAS/cross-nossdav-go/.tools/itu-p1203

/home/quic2/masque-HAS/cross-nossdav-go/.tools/qoe-py310/bin/python \
  -m pip install \
  /home/quic2/masque-HAS/cross-nossdav-go/.tools/itu-p1203
```

如果 `.tools/itu-p1203` 已经存在，不需要重复 clone，直接执行 `pip install` 即可。

### 1.5 验证 P.1203 可用

先把 QoE 环境加入当前终端的 `PATH`：

```bash
export PATH=/home/quic2/masque-HAS/cross-nossdav-go/.tools/qoe-py310/bin:$PATH
```

检查 `p1203-standalone`：

```bash
p1203-standalone --version
```

期望输出类似：

```text
1.11.0
```

检查 `python3 -m itu_p1203`：

```bash
python3 -m itu_p1203 --accept-notice \
  /home/quic2/masque-HAS/cross-nossdav-go/.tools/itu-p1203/examples/mode0.json
```

期望输出 JSON，并包含类似：

```json
"O46": 4.856602221536679
```

再检查 GoDASH 内部使用的取值管道：

```bash
python3 -m itu_p1203 --accept-notice --print-intermediate \
  /home/quic2/masque-HAS/cross-nossdav-go/.tools/itu-p1203/examples/mode0.json \
  | tail -n 6 | head -n1 | cut -f 1 -d ',' | cut -f 4 -d ' '
```

期望输出一个数字，例如：

```text
4.856602221536679
```

### 1.6 启动 GoDASH 客户端

每次打开新终端后，先执行：

```bash
export PATH=/home/quic2/masque-HAS/cross-nossdav-go/.tools/qoe-py310/bin:$PATH
```

然后再运行客户端命令，并保留：

```bash
-QoE on
```

如果要让 P.1203 写出每个分片对应的 JSON 文件并返回真实 P.1203 分数，通常还需要保存 DASH 文件：

```bash
-storeDASH on
```

典型形式是：

```bash
cd /home/quic2/masque-HAS/cross-nossdav-go/cross-layer-implementation/godash-qlogabr

export PATH=/home/quic2/masque-HAS/cross-nossdav-go/.tools/qoe-py310/bin:$PATH

./godash \
  -QoE on \
  -storeDASH on \
  ...
```

其中 `...` 替换为本次实验原有的 URL、ABR、QUIC、输出目录等参数。

### 1.7 常见问题

如果仍然报 `p1203-standalone has not been found in $PATH`，说明当前终端没有执行 `export PATH=...`，或者执行后又换了一个新终端。

如果报 `No module named itu_p1203`，说明当前 `python3` 不是 `.tools/qoe-py310/bin/python3`。可以用下面命令检查：

```bash
which python3
```

期望结果是：

```text
/home/quic2/masque-HAS/cross-nossdav-go/.tools/qoe-py310/bin/python3
```

如果 P.1203 等待输入 `Enter 'accept' to accept:`，说明调用命令缺少 `--accept-notice`。当前仓库的 `qoe/p1203.go` 已经补上该参数；如果你运行的是旧二进制，需要重新编译：

```bash
cd /home/quic2/masque-HAS/cross-nossdav-go/cross-layer-implementation/godash-qlogabr
GOCACHE=/tmp/go-build-cache go build .
```

如果 `go build` 报 `/home/quic2/.cache/go-build` 只读，可以像上面一样指定 `GOCACHE=/tmp/go-build-cache`。

---

## 2. 总体结构

`qoe.CreateQoE(...)` 是该仓库 QoE 计算的统一入口。它按打印头配置决定是否启用各个 QoE 模型，并把结果写入最后一个 `SegPrintLogInformation` 结构体字段：`P1203`、`Clae`、`Duanmu`、`Yin`、`Yu`。同时，它只在**非音频段**条件下执行这些模型；若当前日志条目属于音频，相关 QoE 字段会被置为 `0.0`。([GitHub][2])

从输入上看，这些模型共享同一组会话级统计量，主要来自日志末条记录中累计得到的字段，包括：

* `SegmentRates`、`SumSegRate`：各段码率及其总和
* `TotalStallDur`、`NumStalls`：总卡顿时长与卡顿次数
* `NumSwitches`、`RateChange`、`SumRateChange`：码率切换次数与切换幅度
* `PlaybackTime`：当前播放时长
* `SegmentDuration`：每段时长
* `RepCodec`、`RepWidth`、`RepHeight`：编码与分辨率信息

这些字段决定了各 QoE 指标本质上都是对**质量、卡顿、切换、启动**四类体验因素的不同加权。([GitHub][2])

---

## 3. P.1203

### 3.1 定义

该实现中的 `P.1203` 对应 ITU-T P.1203 体系。源码 `p1203.go` 定义了一个完整的 P.1203 JSON 结构，包含四部分：

* `I11`：音频段信息
* `I13`：视频段信息
* `I23`：卡顿事件信息
* `IGen`：终端与观看环境信息

其中，音频段记录码率、编码、持续时间与开始时间；视频段记录码率、编码、时长、帧率、分辨率与开始时间；`I23` 记录 stalling 序列；`IGen` 固定写入 `device=pc`、`displaySize=1920x1080`、`viewingDistance=150cm`。([GitHub][3])

### 3.2 含义

P.1203 反映的是**面向端到端观看体验的标准化视频 QoE 表征**。该实现不是直接用简单线性式打分，而是先把流媒体会话转换成标准 JSON 描述，再交由 P.1203 计算器处理。其输入覆盖：

* 音频参数
* 视频编码与分辨率
* 段级时间序列
* 卡顿时间序列
* 观看设备参数

因此，P.1203 在该仓库中对应的是最完整、最结构化的一类 QoE 表达。([GitHub][3])

### 3.3 适用范围

`CreateQoE` 对 P.1203 增加了显式兼容性检查。只有在以下条件满足时才允许进入 P.1203 路径：

* 当前条目不是音频
* 视频编码必须是 AVC / H.264
* 分辨率宽高不超过 P.1203 允许范围
* README 进一步说明其适用上限为 `1920x1080`

若不满足，则客户端不会继续该模型。([GitHub][2])

### 3.4 当前仓库中的实现状态

需要特别说明的是，`p1203.go` 中 `createP1203(...)` 会生成 P.1203 JSON 内容。当 `saveFilesBool` 为真时，它会写出 `.json` 文件，并调用 `python3 -m itu_p1203 --accept-notice --print-intermediate ...` 计算返回值。因此，使用 `-QoE on` 时建议同时使用 `-storeDASH on`。如果没有保存文件，当前函数会走未启用的内联计算路径，最后返回 `0.0`。([GitHub][3])

与此同时，仓库 README 的 Vegvisir 使用说明要求安装 `itu-p1203`，并在 `paper-utilities/vegvisir-scripts/cl.py` 的后处理流程中运行 ITU P.1203 计算，把结果写成 `itu-p1203.json`。因此，这个仓库实际上存在两条 P.1203 路径：

1. `godash-qlogabr/qoe/p1203.go`：客户端内联生成 P.1203 JSON，并在保存分片文件时调用本机 `itu_p1203` 计算。
2. Vegvisir 后处理：实验结束后基于结果目录进行正式 P.1203 计算。([GitHub][4])

### 3.5 反映的体验维度

P.1203 在该仓库中综合反映：

* 视频质量水平
* 卡顿事件
* 音视频会话时间结构
* 显示环境

它不是单独强调某一个维度，而是标准化、整体化地反映观看体验。([GitHub][3])

---

## 4. Claye / 日志字段 `Clae`

### 4.1 定义

README 中写作 `Claye`，而日志字段和 `CreateQoE` 中的开关名为 `Clae`。对应实现文件是 `claye.go`。该模型计算式可直接从源码整理为：

[
\text{rateQoE} = 5.67 \cdot \frac{\text{avgRate}}{\text{maxRate}}
]

[
\text{stallPenalty} =
\begin{cases}
4.95 \cdot \left(0.875\left(1+\frac{\ln(nStalls/numSeg)}{6}\right)+0.125\cdot stallDurElem\right), & nStalls>0\
0, & nStalls=0
\end{cases}
]

其中 `stallDurElem` 为平均卡顿时长归一化到 `15s` 上，并在超过 `15s` 时截断为 `1.0`。源码还计算码率序列标准差 `sd`，并定义：

[
\text{switchingPenalty} = 6.72 \cdot \frac{sd}{maxRate}
]

最后总分为：

[
\text{ClayeQoE} = \max\left(0,\ 0.17 + \text{rateQoE} - \text{switchingPenalty} - \text{stallPenalty}\right)
]

([GitHub][5])

### 4.2 含义

该模型是一个**质量收益减去切换惩罚和卡顿惩罚**的归一化综合分数。

其核心特点是：

* 用 `avgRate/maxRate` 表示平均质量占最大可达质量的比例
* 用码率标准差 `sd` 表示质量波动强度
* 用卡顿次数和平均卡顿时长共同构造 stall penalty
* 最终分数下限截断为 `0`

该模型明显强调**平滑性与卡顿惩罚**，而且惩罚是显式建模的。([GitHub][5])

### 4.3 适用范围

该模型依赖的输入全部来自段级日志累计量，因此适用于：

* 连续 VoD 分段播放会话
* 有完整 segment 码率序列与 stall 统计的实验
* 需要比较质量、平滑性与卡顿综合表现的场景

它不依赖外部工具，不要求特定编码，也不要求额外的音视频 JSON 结构。([GitHub][5])

### 4.4 反映的体验维度

Claye 模型主要反映三类因素：

* **平均质量水平**：通过平均码率与最大码率的比值体现
* **质量平滑性**：通过码率标准差体现
* **卡顿强度**：通过卡顿次数与平均时长共同体现

它对应的是“质量—平滑—卡顿”三者平衡型 QoE。([GitHub][5])

---

## 5. Duanmu

### 5.1 定义

`duanmu.go` 中给出的计算式可整理为：

首先定义：

* `initDelay`：前 `initBuffer` 个段的时长总和
* `rebufferPercentage = totalStall / sessionDuration`
* `avgBitrate = (sumSegRate / 1000) / numSeg`
* `avgRateSwitchMagnitude = (sumRateChange / 1000) / nSwitches`，若 `nSwitches=0` 则为 `0`

最终公式为：

[
\text{DuanmuQoE}
================

-2.3 \cdot initDelay
-56.5 \cdot rebufferPercentage
+0.0070 \cdot avgBitrate
+0.0007 \cdot avgRateSwitchMagnitude
+54.0
]

([GitHub][6])

### 5.2 含义

该模型把会话体验拆成四个主要部分：

* **启动时延惩罚**
* **卡顿比例惩罚**
* **平均码率收益**
* **码率切换幅度项**

从系数可以直接看出，这个模型对 `rebufferPercentage` 的惩罚非常强；对 `initDelay` 也给出线性惩罚；平均码率有正收益；切换幅度项在此实现中采用正系数。([GitHub][6])

### 5.3 适用范围

该模型适用于：

* 需要显式考虑**启动阶段代价**的 VoD 实验
* 需要把卡顿标准化为“播放会话占比”而非绝对秒数的场景
* 有完整切换次数、切换幅度和播放时长统计的实验

实现上，它只依赖客户端日志累计字段，不依赖外部分析器。([GitHub][6])

### 5.4 反映的体验维度

Duanmu 模型主要反映：

* **启动成本**
* **会话中卡顿所占比例**
* **平均清晰度**
* **切换幅度**

与 Claye 相比，它更突出**启动时延**与**卡顿占比**。([GitHub][6])

---

## 6. Yin

### 6.1 定义

`yin.go` 中实现的公式可以整理为：

先计算：

* `initDelay`：前 `initBuffer` 个段时长之和
* `avgRateSwitchMagnitude = sumRateChange / 1000`；注意这里**没有除以切换次数**
* `totalStall`：总卡顿时长

然后分两种情况：

若 `PlaybackTime > 0`：

[
\text{YinQoE} = \frac{sumSegRate}{1000} - avgRateSwitchMagnitude - 3 \cdot totalStall
]

若 `PlaybackTime = 0`：

[
\text{YinQoE} = \frac{sumSegRate}{1000} - avgRateSwitchMagnitude - 3 \cdot totalStall - 3000 \cdot initDelay
]

([GitHub][7])

### 6.2 含义

该模型是一个非常直接的**收益减惩罚**型表达：

* 质量收益：累计码率和
* 切换惩罚：累计码率切换幅度
* 卡顿惩罚：总卡顿时长乘以系数 3
* 若播放还未真正开始，则额外施加一个非常大的启动惩罚项 `3000 * initDelay`

该实现特别强调：

* **总质量收益**
* **切换总幅度**
* **卡顿总时长**
* **启动前等待的强惩罚**

([GitHub][7])

### 6.3 适用范围

该模型适用于：

* 需要突出 rebuffer 负面影响的 ABR 比较
* 需要直接反映码率总收益与切换总损失的实验
* 希望把启动前状态与播放中状态区分处理的场景

该模型同样不依赖外部工具，仅使用客户端日志统计。([GitHub][7])

### 6.4 反映的体验维度

Yin 模型主要反映：

* **累计视频质量**
* **码率切换总幅度**
* **总卡顿时长**
* **启动前缓冲等待**

与 Duanmu 相比，它更直接、更“总量化”，而不是比例化。([GitHub][7])

---

## 7. Yu

### 7.1 定义

`yu.go` 的实现首先定义两个模型参数：

* ( w_1 = 1/3 )
* ( w_2 = 20.0 )

随后计算：

* `avgBitrate = (sumSegRate / 1000000) / numSeg`
* `avgRateSwitchMagnitude = (sumRateChange / 1000000) / numSeg`
* `totalDisplayTime = \sum segmentDuration + totalStall`
* `starvation = totalStall / totalDisplayTime`

然后构造：

[
\text{switchingQoE} = w_1 \cdot avgRateSwitchMagnitude
]

[
\text{starvationQoE} = w_2 \cdot starvation
]

最终公式为：

[
\text{YuQoE} = avgBitrate - switchingQoE - starvationQoE
]

([GitHub][8])

### 7.2 含义

该模型把 QoE 分成三部分：

* **平均媒体质量**
* **平均切换代价**
* **饥饿/卡顿占比代价**

这里的 `starvation` 不是总卡顿秒数，而是**总卡顿时间占总显示时间的比例**。因此，Yu 模型非常强调会话中的“播放饥饿程度”。([GitHub][8])

### 7.3 适用范围

该模型适用于：

* 需要比较不同传输路径下“质量—切换—饥饿比例”平衡的实验
* 会话时长足够长、总显示时间有代表性的 VoD 场景
* 需要把 stall 归一化为比例量的分析

同样，它不依赖外部分析器。([GitHub][8])

### 7.4 反映的体验维度

Yu 模型主要反映：

* **平均视频质量**
* **平均切换代价**
* **卡顿占总显示时间的比例**

与 Duanmu 类似，它也使用比例化思想，但这里显式采用 `starvation`，更接近“会话中有多少时间不能顺畅观看”。([GitHub][8])

---

## 8. `qoe6.go`

`qoe6.go` 仅包含一个 `getQoE6(...)`，直接返回常数 `6.000`。在当前 `CreateQoE(...)` 的主调度逻辑中，这个函数没有被纳入五个正式输出指标之列；README 也未将其列入正式日志模型。因此，从当前仓库的正式 QoE 输出体系看，`qoe6.go` 属于未纳入主流程的辅助/遗留实现。([GitHub][9])

---

## 9. 各指标的范围与实现特征

### 9.1 `P.1203`

* 在该仓库客户端内联实现中，当前返回值被固定为 `0.0`
* 真正的 P.1203 结果依赖实验后处理流程生成 `itu-p1203.json`
  ([GitHub][3])

### 9.2 `Claye / Clae`

* 源码显式使用 `max(0, ...)`
* 因此结果下界为 `0`
* 无显式固定上界，但设计上是归一化综合分数
  ([GitHub][5])

### 9.3 `Duanmu`

* 线性组合输出
* 无显式截断
* 可正可负，取决于启动、卡顿和质量水平
  ([GitHub][6])

### 9.4 `Yin`

* 线性组合输出
* 无显式截断
* 当 startup 或 stall 惩罚较大时可出现大负值
  ([GitHub][7])

### 9.5 `Yu`

* 线性组合输出
* 无显式截断
* 结果量纲与平均码率和 starvation 比例有关
  ([GitHub][8])

---

## 10. 仓库当前 QoE 体系的客观总结

该仓库的 QoE 体系由一个统一入口 `CreateQoE(...)` 管理，正式输出五类指标：

* `P.1203`
* `Clae`（实现文件名为 `claye.go`）
* `Duanmu`
* `Yin`
* `Yu`

其中：

* `P.1203` 是标准化 QoE 描述与后处理接口，客户端内部当前返回占位值；
* `Claye` 是“平均质量—切换标准差—卡顿惩罚”型模型；
* `Duanmu` 是“启动时延—卡顿占比—平均码率—切换幅度”型模型；
* `Yin` 是“累计质量—累计切换—总卡顿—启动等待”型模型；
* `Yu` 是“平均质量—平均切换—饥饿比例”型模型。 ([GitHub][2])

这五者共同构成了该仓库对流媒体体验的多视角度量：标准化模型、质量/卡顿平衡模型、启动敏感模型、总量惩罚模型以及 starvation 比例模型。([GitHub][1])

[1]: https://github.com/EDM-Research/cross-that-boundary-mmsys23-nossdav/tree/master/cross-layer-implementation/godash-qlogabr "cross-that-boundary-mmsys23-nossdav/cross-layer-implementation/godash-qlogabr at master · EDM-Research/cross-that-boundary-mmsys23-nossdav · GitHub"
[2]: https://raw.githubusercontent.com/EDM-Research/cross-that-boundary-mmsys23-nossdav/master/cross-layer-implementation/godash-qlogabr/qoe/qoe.go "raw.githubusercontent.com"
[3]: https://raw.githubusercontent.com/EDM-Research/cross-that-boundary-mmsys23-nossdav/master/cross-layer-implementation/godash-qlogabr/qoe/p1203.go "raw.githubusercontent.com"
[4]: https://github.com/EDM-Research/cross-that-boundary-mmsys23-nossdav "GitHub - EDM-Research/cross-that-boundary-mmsys23-nossdav · GitHub"
[5]: https://raw.githubusercontent.com/EDM-Research/cross-that-boundary-mmsys23-nossdav/master/cross-layer-implementation/godash-qlogabr/qoe/claye.go "raw.githubusercontent.com"
[6]: https://raw.githubusercontent.com/EDM-Research/cross-that-boundary-mmsys23-nossdav/master/cross-layer-implementation/godash-qlogabr/qoe/duanmu.go "raw.githubusercontent.com"
[7]: https://raw.githubusercontent.com/EDM-Research/cross-that-boundary-mmsys23-nossdav/master/cross-layer-implementation/godash-qlogabr/qoe/yin.go "raw.githubusercontent.com"
[8]: https://raw.githubusercontent.com/EDM-Research/cross-that-boundary-mmsys23-nossdav/master/cross-layer-implementation/godash-qlogabr/qoe/yu.go "raw.githubusercontent.com"
[9]: https://raw.githubusercontent.com/EDM-Research/cross-that-boundary-mmsys23-nossdav/master/cross-layer-implementation/godash-qlogabr/qoe/qoe6.go "raw.githubusercontent.com"
