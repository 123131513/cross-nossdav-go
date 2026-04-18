# Pensieve 复现方案调整说明

本文档说明 Pensieve 在本项目中的复现方案为何调整为“官方仓库子模块 + GoDASH 播放器侧兼容层”。

## 1. 调整原因

之前的方向是在当前仓库内维护一份 `pensieve_service.py`，用来近似官方 `rl_server`。

这个做法的问题是：

1. 服务代码在两个仓库里重复维护
2. 很容易和官方 `hongzimao/pensieve` 逐渐漂移
3. 当你们已经 fork 了 Pensieve 时，没有必要继续在当前仓库保留一份副本

因此现在改为：

- 直接把 `https://github.com/123131513/pensieve` 接为子模块
- 运行子模块中的官方 `rl_server/rl_server_no_training.py`
- 当前仓库只维护 GoDASH 到 Pensieve 的调用链

## 2. 当前复现边界

当前分工是：

- `GoDASH`
  - 下载 chunk
  - 维护 buffer / stall / 日志
  - 在每段下载后采集官方 Pensieve 所需字段
  - 向外部 Pensieve 服务发请求
- `Pensieve 子模块`
  - 提供官方训练 / 测试 / `rl_server` 代码
  - 加载 checkpoint
  - 执行 actor 网络推理

这比“在当前仓库复制一个服务脚本”更接近论文原始实现的组织方式。

就当前仓库和当前机器的实际状态而言，还需要明确区分两件事：

1. 代码组织已经切换为“子模块 + 兼容层”
2. 官方 `rl_server` 还没有在当前机器上跑起来，因为当前机器缺少 Python 2 / TensorFlow 1.x 环境

## 3. 为什么当前播放器还需要一层兼容代码

因为当前播放器不是原始 `dash_client`，而是 `GoDASH`。

所以仍然需要一层很薄的适配逻辑，负责：

1. 把 GoDASH 的本地表示索引映射成 Pensieve 的 `0..5`
2. 将 chunk 下载结果整理成官方 `rl_server` 能识别的字段
3. 把服务返回的动作映射回本地表示索引

这层代码不重写策略，只做协议与索引映射。

## 4. 严格一致复现的条件

如果要说“严格使用官方 Pensieve 方案”，当前至少需要满足：

1. 启动的是子模块里的 `rl_server/rl_server_no_training.py`
2. 使用的是官方或你们明确管理的 checkpoint
3. 码率梯度与官方 `VIDEO_BIT_RATE = [300, 750, 1200, 1850, 2850, 4300]` 一致
4. 视频 chunk 大小假设不偏离官方服务太多，或者你们明确接受这种实验边界

其中第 3 点是当前播放器侧已经强约束的。

另外还需要接受一个官方实现层面的约束：

- `rl_server/rl_server_no_training.py` 依赖旧版 Python/TensorFlow 环境
- 直接使用 `python3` 运行该脚本会失败

## 5. 这次方案的直接收益

这样调整后有三个明显收益：

1. 当前仓库不再维护一份 Pensieve 服务副本
2. 论文复现所依赖的核心服务代码有独立来源，版本更清晰
3. 后续如果要修 Pensieve，只需要更新子模块仓库和子模块指针

但它并没有消除官方实现本身的老环境依赖，所以运行成功仍然需要额外准备旧版 Python / TensorFlow 环境。

## 6. 结论

现在的方案不是“在当前仓库重写 Pensieve”，而是：

- 用当前仓库承载播放器和跨层实验逻辑
- 用子模块承载官方 Pensieve 方案代码

这更适合做可追溯、可维护的论文复现。
