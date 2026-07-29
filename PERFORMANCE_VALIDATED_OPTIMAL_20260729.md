# CubeSandbox 已验证最优源码清单

日期：2026-07-29

## 1. 仓库口径

- 上游基线：CubeSandbox `v0.5.1`，`a164417f497234a0d787cb328b0ae96480b1569b`
- 汇总分支：`perf/validated-optimal-20260729`
- 目标场景：ARM64/openEuler guest，2U2G Template，1 GiB writable layer
- OCI 基线：`cubesandbox-bench/sandbox-code-envd-ci:arm64-slim`
- 正式状态：cross-stage early probe 关闭；保留标准 Template Probe

本分支只汇总已经完成成功率、性能或功能门禁的修改。源码提交来自
v0.5.1 同机 A/B 和后续 `.90` OCI/Template 实验；没有把仅在失败样本上
变快、导致 reset timeout、破坏 `run_code` 或破坏 Snapshot clone 的候选纳入。

## 2. 保留的提交

| 提交 | 优化 | 保留理由 |
|---|---|---|
| `768a14f` | 日志级别解析忽略大小写/空白 | 避免 `info` 回退到 DEBUG；c50 日志增量约 31 MiB 降至 4.24 MiB |
| `509cda2` | HTTP/TCP 启动阶段最多两次 25 ms quick retry | 消除首次 Probe 略早时的完整 500 ms 等待，不改变失败阈值语义 |
| `c87cbff` | 跳过无 virtio-fs/传播挂载工作的 restore RPC | guest 中已有 restore 状态时不再执行空 `CreateSandbox/CreateContainer` |
| `4e568d5` | 并行 guest 时间/RNG reset 和可重叠 restore setup | 只并行相互独立的 guest-agent RPC |
| `62836e7` | 按需创建 health client | `check_agent=false` 时不再建立无用控制连接 |
| `4ce3bbb` | CubeShim 日志批量写和单 Tokio worker | 降低 c50 下逐消息 flush、线程和调度竞争 |
| `b3301f8` | ARM64 GIC/MSI-X 路由批量恢复 | 合并 KVM 路由刷新；不依赖宿主机 GICv4 参数 |
| `a8ee9c7` | envd Snapshot-only MMDS-prime | 保留 Template 构建/reset 稳定性所需活动，restore 后停止无效 MMDS 轮询 |
| `51d36b0` | native code server v3 | 49999 监听前等待 envd，Template 保存 ready cache，保持 NDJSON execute 协议 |
| `b7d9402` | ARM64 OCI 多阶段源码构建 | 锁定 base digest、envd commit 和 Go 1.26.2，不提交预编译二进制 |

## 3. 已验证数据

### 3.1 CubeSandbox 核心 restore 优化

v0.5.1 核心优化组合在相同 2U2G Template 下连续三轮 c50/n500：

| 轮次 | 成功 | avg (ms) |
|---|---:|---:|
| run 1 | 500/500 | 192.969 |
| run 2 | 500/500 | 194.858 |
| run 3 | 500/500 | 198.194 |
| 三轮均值 | 1500/1500 | 195.340 |

### 3.2 MMDS-prime 稳定性门禁

保留的 50 ms deadline 检查版本连续三轮 c50/n500：

| 轮次 | 成功 | avg (ms) | p95 (ms) |
|---|---:|---:|---:|
| run 1 | 500/500 | 199.59 | 586.34 |
| run 2 | 500/500 | 202.92 | 576.71 |
| run 3 | 500/500 | 205.64 | 579.15 |

1 ms cancel、20 ms grace、timerfd clean-stop 等更激进版本均出现 reset timeout、
HTTP 408 或残留 shim，因此未保留。

### 3.3 code server v3，early probe 关闭

正式矩阵每项测试前都清理 sandbox/shim/task，并确认 1000 TAP、0 in-use：

| 场景 | 成功 | avg (ms) | p95 (ms) | max (ms) | QPS |
|---|---:|---:|---:|---:|---:|
| c1/n20 | 20/20 | 39.000 | 46.207 | 48.172 | 20.948 |
| c10/n200 | 200/200 | 53.845 | 103.107 | 115.955 | 164.887 |
| c20/n300 | 300/300 | 58.695 | 78.074 | 110.980 | 289.338 |
| c50/n500 | 500/500 | 138.610 | 237.325 | 801.969 | 273.828 |

c50 额外重复三轮后的四轮汇总：

| 轮次 | 成功 | avg (ms) | p95 (ms) | QPS |
|---|---:|---:|---:|---:|
| matrix | 500/500 | 138.610 | 237.325 | 273.828 |
| repeat 2 | 500/500 | 138.301 | 223.725 | 266.738 |
| repeat 3 | 500/500 | 137.679 | 228.059 | 238.750 |
| repeat 4 | 500/500 | 139.936 | 238.600 | 276.763 |
| 四轮均值 | 2000/2000 | 138.631 | 231.927 | 264.020 |

early probe 关闭后的 benchmark 合计 2520/2520，无 HTTP 408、
`reset guest time failed` 或残留资源。真实 SDK create 后立即 `run_code` 另有
3/3 通过，stdout/result 分别为 `12345`/`42`。

## 4. 明确排除的候选

- cross-stage early probe：平均 c50 仅再改善 3.22%，已按要求回退；这里保留的
  `509cda2` 是标准 Probe 内部的有界 quick retry，不是同一个实验。
- GICv4、vtimer IRQ bypass、`nohlt`：收益不足或造成宿主机失联/创建失败；
  `b3301f8` 只是 VMM 内部路由批处理，不开启这些内核参数。
- `maxcpus=1`：可规避部分失败但牺牲 2U/3U/4U Template 语义。
- 复用 guest-agent 控制连接转发 init 日志：c50 很快，但高并发 `run_code`
  出现秒级延迟和超时。
- 延迟异步专用日志连接：避免了上述控制连接争用，但动态 Snapshot clone
  出现 GICv3 ITS restore `EINVAL`。
- 社区 guest image：多轮出现 `reset guest time failed`；正式运行仍要求已验证的
  openEuler `cube-guest-image-cpu.img` 和 openEuler `vmlinux-bm`。

## 5. 构建与运行边界

优化 OCI 源码位于
`examples/code-sandbox-quickstart/images/arm64-performance/`。其中 Dockerfile
从锁定的 envd upstream commit 应用补丁，并分别编译 ARM64 envd 和 native
code server；仓库不包含本地实验二进制。

openEuler guest kernel、guest image、1000 TAP 池、CubeSandbox 服务配置和
Template/Snapshot 数据属于部署产物，不应提交进源码仓库。更换任一 guest/OCI
层后必须重新构建 Template，旧 Template 不会自动继承新文件。

本分支是“各项已验证修改的源码汇总”。核心源码组合和 OCI v3 分别有上述远端
压力数据；新分支的精确最终 commit 尚未重新部署执行一次全矩阵，因此后续发布前
仍应在 `.90` 以同一资源清理门禁做一次组合回归，不能把不同阶段数据误写成一次
相同二进制的测试结果。
