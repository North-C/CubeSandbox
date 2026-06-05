# Arm64 create-only 调优阶段总结

记录时间：2026-06-03

远端机器：`root@192.168.25.61`

测试口径：`cube-bench --mode create-only`，模板 `tpl-arm64-bench-ubuntu2204`。

## 当前结论

截至目前，create-only 并发慢的问题已经从最初的 network-agent/CubeVS 路径转移到 CubeShim 的 `RestoreVm` 路径。

最新链路关联显示：

- CubeAPI/CubeMaster 调度和请求转发不是主瓶颈。
- network-agent/CubeVS 热路径已经被压到毫秒级。
- Cubelet `runContainer.new_task_ms` 与 CubeShim `CreatePodSandbox -> RestoreVm` 长尾基本重合。
- 当前 c100 create-only 的端到端 p99 约 `2.5-2.7s`，主体来自并发 VMM snapshot restore。

## 已完成优化

### network-agent / CubeVS

已验证并保留的优化：

- 跳过池化 TAP 的重复 `AttachFilter`。
- 为 CubeVS pinned program/map 引入 cache。
- 为 inner map 使用 `(mapName, ifindex)` key cache，跳过 outer hash-of-maps lookup。
- 跳过池化 TAP 的重复 ARP `NeighSet`。

效果：

- `populateInnerMap` 从几十毫秒降到约 `0.01ms`。
- `registerCubeVSTap` p99 降到约 `0.1ms`。
- `restoreTap.add_arp_ms` 降到 `0`。

这些优化使网络侧不再是 c50/c100 create-only 的主瓶颈。

### 上层阶段埋点

已加入阶段耗时：

- CubeAPI create handler/service/CubeMaster client。
- CubeMaster HTTP create、scheduler、Cubelet RPC。
- Cubelet service.Create、workflow step/action。
- Cubelet cubebox/containerd `NewTask`。
- network-agent/CubeVS 热路径。

这些埋点将瓶颈定位到 Cubelet `runContainer.new_task_ms`，进一步关联到 CubeShim `RestoreVm`。

## 关键验证结果

### 已采用优化结果

| 版本 | 场景 | 成功率 | p99 |
| --- | --- | ---: | ---: |
| inner-key-cache | c50 n100 | 100% | 约 `536ms` |
| inner-key-cache | c100 n200 | 100% | 约 `2190ms` |
| ARP-skip | c50 n100 | 100% | 约 `546ms` |
| ARP-skip | c100 n200 | 100% | 约 `2578ms` |
| 上层埋点复测 | c50 n100 | 100% | 约 `543ms` |
| 上层埋点复测 | c100 n200 | 100% | 约 `2668ms` |

### Cubelet/CubeShim 分段

c100 窗口内：

| 阶段 | 样本 | p99 |
| --- | ---: | ---: |
| Cubelet `service.Create total` | 300 | 约 `2663ms` |
| Cubelet `runContainer total` | 300 | 约 `2660ms` |
| Cubelet `runContainer.new_task_ms` | 300 | 约 `2659ms` |
| CubeShim `CreatePodSandbox` | 300 | 约 `2654ms` |
| CubeShim `RestoreVm` | 300 | 约 `2640ms` |
| CubeShim `LaunchVmm` | 300 | 约 `2ms` |
| CubeShim `CreateContainer` | 300 | 约 `5ms` |

判断：`containerd.NewTask` 的主要耗时是 CubeShim/VMM snapshot restore。

## 已验证但不采用的方案

### workflow create 并发降到 50

远端临时将 Cubelet workflow create `concurrent` 从 `100` 改为 `50` 后验证：

- c50 n100：p99 约 `664ms`，成功率 `99%`。
- c100 n200：p99 约 `3945ms`，成功率 `100%`。

结论：

- 简单降低 workflow create 并发会把排队时间计入 API 延迟。
- 对 c100 端到端 p99 是负优化。
- 该配置已恢复为 `100`。

### 手动预热 snapshot memory-ranges

远端手动顺序读取：

```bash
dd if=/usr/local/services/cubetoolbox/cube-snapshot/cubebox/tpl-arm64-bench-ubuntu2204/1C512M/snapshot/memory-ranges of=/dev/null bs=16M
```

结果：

- 512MiB 读取约 `56ms`，约 `9.5GB/s`。
- c100 n200 复测 p99 约 `2372ms`，但成功率 `99%`。

结论：

- `memory-ranges` 已基本在页缓存中，普通冷读不是 2s 长尾主因。
- 手动预热没有通过稳定性验证，暂不作为正式优化。

## 新增 VMM restore 分段验证

### 验证内容

已在 `hypervisor/vmm` 里增加临时分段计时日志，覆盖：

- `Vmm::vm_restore`
- `Vm::new_from_snapshot`
- `MemoryManager::new_from_snapshot`
- `MemoryManager::new`
- `Vm::new_from_memory_manager`
- `Vm::restore`

本地 x86 验证：

```bash
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 验证：

- 构建目录：`/opt/cubesandbox-build/upper-create-timing-20260602-src`
- 构建命令：`cd CubeShim && cargo build --release --locked`
- 构建耗时：`4m23s`
- 部署文件：`/usr/local/services/cubetoolbox/cube-shim/bin/containerd-shim-cube-rs`
- 新 shim sha256：`2dfc8beffba7117ee9d702465a50fdb0549bb5ec538fc1e9283deadbfe19b344`
- 旧 shim 备份：`containerd-shim-cube-rs.bak-20260603114150`

### smoke 验证

命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restore-timing-smoke-c1-n1 1 1
```

结果：

- 成功率：`100%`
- create p99：`36.758ms`
- 清理：`1/1`
- VMM `restore timing` 日志正常输出。

单样本 VMM 分段：

| 阶段 | 耗时 |
| --- | ---: |
| `memory_manager_new_from_snapshot.total_ms` | `0.114ms` |
| `vm_new_from_snapshot.total_ms` | `0.532ms` |
| `vm_restore.total_ms` | `11.526ms` |
| `vmm_vm_restore.total_ms` | `12.511ms` |
| `vm_restore.restore_devices_ms` | `7.501ms` |
| `vm_restore.vgic_restore_ms` | `1.098ms` |

### 并发验证

命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restore-timing-c50-n100 50 100
./run_create_only_case.py restore-timing-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c50 n100 | `100%` | `565.6ms` | `100/100` |
| c100 n200 | `98.5%` | `2470ms` | `197/197` |

c100 的 3 个失败均为 `130459` 初始化失败，和此前预热测试中的少量失败模式一致；成功样本仍可用于 restore 阶段分布分析。

### VMM 分段结论

c50 n100：

| 阶段 | p99 |
| --- | ---: |
| `vmm_vm_restore.total_ms` | `529.889ms` |
| `vm_restore.total_ms` | `528.791ms` |
| `vm_restore.restore_devices_ms` | `293.556ms` |
| `vm_restore.vgic_restore_ms` | `370.568ms` |
| `vm_new_from_snapshot.total_ms` | `0.866ms` |
| `memory_manager_new_from_snapshot.total_ms` | `0.147ms` |

c100 n200：

| 阶段 | p99 |
| --- | ---: |
| `vmm_vm_restore.total_ms` | `2416.883ms` |
| `vm_restore.total_ms` | `2415.850ms` |
| `vm_restore.restore_devices_ms` | `1196.306ms` |
| `vm_restore.vgic_restore_ms` | `1508.526ms` |
| `vm_new_from_snapshot.total_ms` | `0.797ms` |
| `memory_manager_new_from_snapshot.total_ms` | `0.152ms` |

判断：

- fast restore 已确认生效，`memory-ranges` 使用 `MAP_PRIVATE` 文件映射，`fill_saved_regions_ms=0`。
- 内存映射、KVM VM 创建、CPU manager 初始化均不是主瓶颈，p99 基本在 1ms 内。
- 并发长尾集中在 `Vm::restore`。
- `Vm::restore` 内的主要放大项是 ARM64 `restore_vgic_and_enable_interrupt` 和 `DeviceManager::restore_devices`。
- `restore_devices` 当前每个 VM 创建一个 Tokio multi-thread runtime，线程数最多 `MAX_WORKER_THREADS=5`；c100 下可能造成大量临时 worker 线程和调度压力。
- `restore_vgic_and_enable_interrupt` 是 ARM64 KVM/GIC ioctl 路径，需要继续拆分 `create_vgic`、`init_pmu`、`set_gicr_typers`、`restore(GIC state)`、`enable`。

## 后续方向

下一步继续分析和验证：

1. 给 `restore_vgic_and_enable_interrupt` 增加更细分计时，确认 GIC 具体慢点。
2. 给 `DeviceManager::restore_devices` / `restore_device_node` 增加更细分计时，确认 runtime 创建、device tree traversal、group restore、单设备 pause/restore 的真实占比。
3. 如果 `restore_devices` 慢在临时 Tokio runtime 和 worker 线程创建，优先验证同步恢复或减少 worker 数是否降低 c100 tail。
4. 如果 `vgic_restore` 慢在 KVM ioctl，则继续评估 ARM64 GIC restore 能否减少重复 ioctl、延后 enable，或通过并发闸门降低内核侧排队。

## 第二层 VMM 分段验证

继续在 VMM 内增加细分计时，覆盖：

- `restore_vgic_and_enable_interrupt` 内部的 `create_vgic`、`init_pmu`、`set_gicr_typers`、`restore_gic_state`、`enable_interrupt`。
- `DeviceManager::restore_devices` 内部的 device graph 收集、Tokio runtime 创建、`block_on`、group restore。
- `restore_device_node` 内部的单设备 lock、pause、restore。

远端 ARM64 构建：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：约 `4m02s`
- 新 shim sha256：`4321368cd695e3b18cbabeaeb72dcecc9de2f898677b1662d3073f6b75513947`
- 旧 shim 备份：`containerd-shim-cube-rs.bak-20260603115215`

smoke：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restore-detail-smoke-c1-n1 1 1
```

结果：

- create p99：约 `39.7ms`
- 清理：`1/1`
- `vgic_restore_detail.total_ms=1.088ms`
- `vgic_restore_detail.enable_interrupt_ms=0.548ms`
- `device_manager_restore_devices.total_ms=10.449ms`

c50/c100：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restore-detail-c50-n100 50 100
./run_create_only_case.py restore-detail-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c50 n100 | `100%` | `549.7ms` | `100/100` |
| c100 n200 | `100%` | `2524.6ms` | `200/200` |

c50 n100 内部分段：

| 阶段 | p99 |
| --- | ---: |
| `vgic_restore_detail.total_ms` | `331.028ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `330.534ms` |
| `vgic_restore_detail.restore_gic_state_ms` | `0.613ms` |
| `device_manager_restore_devices.total_ms` | `300.219ms` |
| `device_restore_node_total.runtime_build_ms` | `0.777ms` |
| `device_restore_node_total.block_on_ms` | `299.726ms` |

c100 n200 内部分段：

| 阶段 | p99 |
| --- | ---: |
| `vgic_restore_detail.total_ms` | `1809.389ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `1808.911ms` |
| `vgic_restore_detail.restore_gic_state_ms` | `0.531ms` |
| `device_manager_restore_devices.total_ms` | `1460.382ms` |
| `device_restore_node_total.runtime_build_ms` | `2.819ms` |
| `device_restore_node_total.block_on_ms` | `1459.930ms` |

判断：

- GIC 路径的并发长尾几乎全部来自 `enable_interrupt_ms`，不是 GIC state restore。
- `restore_devices` 的并发长尾在 `block_on` 内，不是每 VM 创建 Tokio runtime 的直接成本。
- 单设备聚合显示，慢点主要集中在 `_virtio-pci-*` 包装设备 restore，不是底层 pmem/tap/vsock 自身的 pause，也不是锁等待。

## GIC 批量路由优化验证

尝试优化：

- 在 `InterruptSourceGroup` 增加 `update_many` 默认接口。
- 在 `MsiInterruptGroup` 增加批量更新实现，将多条 route insert 后合并为一次 `set_gsi_routes`。
- 在 ARM64 `Gic::enable()` 中将 32 个 legacy IRQ route 从逐条 `update` 改为一次 `update_many`。

本地 x86 验证：

```bash
cargo fmt --manifest-path hypervisor/Cargo.toml --package vm-device --package vmm --package devices
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：约 `4m04s`
- 新 shim sha256：`a929e10c9fe2c5318aa1ec896ec9adfeec1b6becf1f5354dba6564fb23c9e5bb`
- 旧 shim 备份：`containerd-shim-cube-rs.bak-20260603120216`

smoke：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py gic-batch-smoke-c1-n1 1 1
```

结果：

- create p99：`33.4ms`
- 清理：`1/1`
- `vgic_restore_detail.enable_interrupt_ms=0.078ms`

c100：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py gic-batch-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c100 n200 | `100%` | `2955.9ms` | `200/200` |

按本 case 的 sandbox id 过滤 VMM 日志后：

| 阶段 | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| `vmm_vm_restore.total_ms` | `1739.418ms` | `2747.956ms` | `2922.781ms` | `3036.880ms` |
| `vm_restore.restore_devices_ms` | `658.432ms` | `1414.748ms` | `1505.900ms` | `1552.833ms` |
| `vm_restore.vgic_restore_ms` | `762.730ms` | `1895.667ms` | `1983.359ms` | `2046.193ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `762.191ms` | `1895.154ms` | `1982.824ms` | `2045.677ms` |
| `device_restore_node_total.block_on_ms` | `657.917ms` | `1414.258ms` | `1505.354ms` | `1552.298ms` |

`device_restore_node` 单设备 p99 前几项：

| 设备 | p99 |
| --- | ---: |
| `_virtio-pci-virtio_rw` | `1153.763ms` |
| `_virtio-pci-pmem-cubebox-image-0` | `1067.493ms` |
| `_virtio-pci-virtio_ro` | `1036.573ms` |
| `_virtio-pci-tap-0` | `1022.049ms` |
| `_virtio-pci-vsock` | `991.688ms` |
| `_virtio-pci-__console` | `803.304ms` |

判断：

- 该优化可以保持 smoke 功能正常，但 c100 下 `enable_interrupt_ms` p99 未下降，端到端 p99 反而波动到约 `3s`。
- 因此，单独 batching GIC legacy IRQ route 不是当前并发尾延迟的有效优化，暂不作为已验证收益提交。
- 后续应继续检查 `Gic::enable()` 内除 legacy IRQ route 外的 KVM ioctl 路径，以及 `_virtio-pci-*` restore/activate 阶段是否仍有 MSI/MSI-X route、irqfd/ioeventfd 等高并发 ioctl 风暴。

## MSI-X restore 批量路由验证

尝试优化：

- 只修改 snapshot restore 路径 `MsixConfig::set_state()`。
- 恢复状态时先收集未 masked MSI-X vector 配置。
- 将原来的多次 `update()+enable()` 改为一次 `update_many()` 加一次 `enable()`。
- 保留运行时 `set_msg_ctl()` 逐条更新逻辑，避免改变 guest 正常驱动配置阶段语义。

本地 x86 验证：

```bash
cargo fmt --manifest-path hypervisor/Cargo.toml --package pci --package vm-device --package vmm --package devices
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建和部署：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：约 `4m04s`
- 新 shim sha256：`aef8672ebcbfbcc76b7f0ee4238ba2e43d48a6615d3ca234e3862136e377b3c3`
- 新 cube-runtime sha256：`06010ecd52b9e031517589159a7448b15be60474ba98b0262b50d6d3581543eb`
- 旧 shim 备份：`containerd-shim-cube-rs.bak-20260603135437`

smoke：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py msix-batch-smoke-c1-n1 1 1
```

结果：

- create p99：`34.7ms`
- 清理：`1/1`

c100：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py msix-batch-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c100 n200 | `100%` | `2506.6ms` | `200/200` |

按本 case 的 sandbox id 过滤 VMM 日志后：

| 阶段 | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| `vmm_vm_restore.total_ms` | `1578.066ms` | `2305.246ms` | `2479.525ms` | `2603.365ms` |
| `vm_restore.restore_devices_ms` | `870.225ms` | `1282.306ms` | `1337.693ms` | `1544.240ms` |
| `vm_restore.vgic_restore_ms` | `516.264ms` | `1383.692ms` | `1558.221ms` | `1730.919ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `515.740ms` | `1383.169ms` | `1557.715ms` | `1730.361ms` |
| `device_restore_node_total.block_on_ms` | `869.728ms` | `1281.733ms` | `1337.144ms` | `1543.680ms` |

`device_restore_node` 单设备 p99 前几项：

| 设备 | p99 |
| --- | ---: |
| `_virtio-pci-tap-0` | `810.119ms` |
| `_virtio-pci-pmem-cubebox-image-0` | `794.859ms` |
| `_virtio-pci-cube-fs` | `741.447ms` |
| `_virtio-pci-disk-0` | `715.400ms` |
| `_virtio-pci-__rng` | `685.824ms` |
| `_virtio-pci-_pmem0` | `673.755ms` |
| `_virtio-pci-__console` | `670.464ms` |
| `_virtio-pci-virtio_rw` | `667.583ms` |
| `_virtio-pci-vsock` | `514.646ms` |

判断：

- MSI-X restore 批量化对 `_virtio-pci-*` 单设备 restore 有正向效果，原先多个设备 p99 约 `1.0-1.15s`，本轮降到约 `0.5-0.81s`。
- `restore_devices` p99 从 GIC batch 版本的约 `1505ms` 降到约 `1338ms`，但端到端 p99 仍约 `2.5s`，没有形成数量级改善。
- 当前更大的尾部项仍是 `vgic_restore_detail.enable_interrupt_ms`，p99 约 `1558ms`。
- 下一步应直接细分 `MsiInterruptGroup::enable()` / `InterruptRoute::enable()`，确认 `enable_interrupt_ms` 是否主要由 32 次 legacy IRQ `register_irqfd` 或所有 MSI-X route 的 `register_irqfd` 并发排队造成。

## irqfd / GSI routing 细分验证

新增诊断：

- `MsiInterruptGroup::enable()` 输出 route 数、实际新注册 irqfd 数、已注册数、`register_irqfd_ms`、单 route 最大耗时。
- `MsiInterruptGroup::update_many()` 输出 route update 数、实际新注册 irqfd 数、`set_gsi_routes_ms`。
- `Gic::enable()` 输出 legacy IRQ enable 和 legacy route update 两段耗时。

本地 x86 验证：

```bash
cargo fmt --manifest-path hypervisor/Cargo.toml --package pci --package vm-device --package vmm --package devices
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建和部署：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：约 `4m00s`
- 新 shim sha256：`402358cc78ca177be3fae737cc7adbaed1f546d94cadd79c0941111182e62458`
- 新 cube-runtime sha256：`82926c92042eb80cb62baa6b1e191d7ecc90fe5bef60d0da89927ae6acbf64f2`
- 旧 shim 备份：`containerd-shim-cube-rs.bak-20260603140342`

smoke：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py irqfd-detail-smoke-c1-n1 1 1
```

结果：

- create p99：`32.7ms`
- 清理：`1/1`
- 日志字段完整。

c100：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py irqfd-detail-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c100 n200 | `99.0%` | `2477ms` | `198/198` |

2 个失败均为此前已见过的 `130459` 初始化失败；成功样本仍用于阶段分布分析。

按本 case 的 198 个成功 sandbox id 过滤 VMM 日志后：

| 阶段 | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| `vmm_vm_restore.total_ms` | `1538.072ms` | `2208.604ms` | `2448.550ms` | `2460.245ms` |
| `vm_restore.restore_devices_ms` | `874.610ms` | `1275.250ms` | `1334.013ms` | `1356.986ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `537.933ms` | `1389.915ms` | `1517.745ms` | `1560.110ms` |
| `gic_enable_detail.legacy_enable_ms` | `537.842ms` | `1389.802ms` | `1517.641ms` | `1560.018ms` |
| `gic_enable_detail.legacy_route_update_ms` | `0.084ms` | `0.116ms` | `0.128ms` | `0.144ms` |

跨所有 interrupt group 的聚合：

| 阶段 | 字段 | p50 | p95 | p99 | max |
| --- | --- | ---: | ---: | ---: | ---: |
| `interrupt_group_enable` | `register_irqfd_ms` | `0.000ms` | `507.313ms` | `1275.420ms` | `1560.004ms` |
| `interrupt_group_enable` | `max_route_ms` | `0.000ms` | `44.747ms` | `203.888ms` | `285.169ms` |
| `interrupt_group_update_many` | `register_irqfd_ms` | `66.984ms` | `169.757ms` | `240.764ms` | `355.660ms` |
| `interrupt_group_update_many` | `set_gsi_routes_ms` | `0.059ms` | `0.086ms` | `0.104ms` | `0.122ms` |

按 `config_count` 拆分 `update_many`：

| `config_count` | `total_ms` p99 | `register_irqfd_ms` p99 | `set_gsi_routes_ms` p99 |
| ---: | ---: | ---: | ---: |
| 2 | `756.238ms` | `228.350ms` | `0.097ms` |
| 3 | `707.477ms` | `259.885ms` | `0.100ms` |
| 4 | `406.677ms` | `220.450ms` | `0.093ms` |
| 32 | `0.123ms` | `0.000ms` | `0.113ms` |

判断：

- GIC `enable_interrupt_ms` 的并发长尾几乎全部来自 `legacy_enable_ms`，即 32 路 legacy IRQ 的 `register_irqfd`。
- `legacy_route_update_ms` 和 `set_gsi_routes_ms` 都在 `0.1ms` 级，不是主因。
- `_virtio-pci-*` 设备侧虽然经过 MSI-X restore 批量路由后有所下降，但 `update_many` 内仍会为每个 MSI-X vector 调用 `register_irqfd`；这部分在 c100 下也出现百毫秒级长尾。
- 当前优化优先级应从 GSI routing batching 转向减少或推迟 irqfd 注册数量，以及降低恢复阶段并发 irqfd 注册风暴。

## 选择性 legacy IRQ 注册验证

尝试优化：

- 给 `InterruptController` 增加默认 no-op 的 `register_legacy_irq()`。
- `LegacyUserspaceInterruptManager::create_group()` 在设备创建 legacy interrupt group 时登记实际使用的 IRQ。
- ARM64 `Gic` 保存已登记的 legacy IRQ 集合。
- `Gic::enable()` 从固定注册 32 路 legacy IRQ，改为只对已登记 IRQ 调用 `enable_selected()` 和 `update_many()`。

该优化针对上一轮诊断确认的热点：create-only 模板实际只使用 4 路 legacy IRQ，但原实现每个 VM restore 都固定注册 32 路 irqfd。

本地 x86 验证：

```bash
cargo fmt --manifest-path hypervisor/Cargo.toml --package devices --package vm-device --package vmm --package pci
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建和部署：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：约 `4m05s`
- 新 shim sha256：`3ffb82fd2f401564f5d3f3d3ac9dd70d30323c7311afd9302d5f10a6b78fe922`
- 新 cube-runtime sha256：`1d05f949a96865a999b97fcd445491c8f240afad38518874fc058196b30b8a70`
- 旧 shim 备份：`containerd-shim-cube-rs.bak-20260603141352`

smoke：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py selective-legacy-smoke-c1-n1 1 1
```

结果：

- create p99：`32.8ms`
- 清理：`1/1`
- `gic_enable_detail.legacy_irq_count=4`
- `vgic_restore_detail.enable_interrupt_ms=0.055ms`

c100：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py selective-legacy-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c100 n200 | `99.5%` | `910ms` | `199/199` |

1 个失败为此前已见过的 `130459` 初始化失败；成功样本用于阶段分布分析。

按本 case 的 199 个成功 sandbox id 过滤 VMM 日志后：

| 阶段 | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| `vmm_vm_restore.total_ms` | `424.261ms` | `709.226ms` | `794.636ms` | `834.004ms` |
| `vm_restore.restore_devices_ms` | `355.631ms` | `659.136ms` | `742.425ms` | `785.484ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `19.218ms` | `110.098ms` | `113.782ms` | `115.231ms` |
| `gic_enable_detail.legacy_irq_count` | `4` | `4` | `4` | `4` |
| `gic_enable_detail.legacy_enable_ms` | `19.149ms` | `110.039ms` | `113.717ms` | `115.158ms` |
| `gic_enable_detail.legacy_route_update_ms` | `0.050ms` | `0.089ms` | `0.104ms` | `0.131ms` |
| `device_restore_node_total.block_on_ms` | `355.072ms` | `658.615ms` | `741.952ms` | `784.954ms` |

`device_restore_node` 单设备 p99 前几项：

| 设备 | p99 |
| --- | ---: |
| `_virtio-pci-tap-0` | `437.554ms` |
| `_virtio-pci-cube-fs` | `417.992ms` |
| `_virtio-pci-disk-0` | `415.821ms` |
| `_virtio-pci-pmem-cubebox-image-0` | `413.866ms` |
| `_virtio-pci-__console` | `408.227ms` |
| `_virtio-pci-__rng` | `403.033ms` |
| `_virtio-pci-_pmem0` | `383.586ms` |
| `_virtio-pci-virtio_ro` | `325.988ms` |
| `_virtio-pci-vsock` | `212.075ms` |

判断：

- 选择性 legacy IRQ 注册是目前收益最明确的优化。
- ARM64 GIC restore 中固定 32 路 legacy IRQ `register_irqfd` 的并发风暴被缩小到实际使用的 4 路。
- `vgic_restore_detail.enable_interrupt_ms` p99 从约 `1518ms` 降到约 `114ms`。
- `vmm_vm_restore.total_ms` p99 从约 `2449ms` 降到约 `795ms`。
- create-only c100 端到端 p99 从约 `2477ms` 降到约 `910ms`。
- 剩余主要瓶颈已转移到 `_virtio-pci-*` restore 中的 MSI-X irqfd 注册，`restore_devices_ms` p99 约 `742ms`。

稳定复测：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py selective-legacy-repeat-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c100 n200 | `100%` | `867.2ms` | `200/200` |

内部关键数据：

| 阶段 | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| `vmm_vm_restore.total_ms` | `438.806ms` | `658.550ms` | `674.076ms` | `686.799ms` |
| `vm_restore.restore_devices_ms` | `372.971ms` | `603.253ms` | `616.122ms` | `637.187ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `15.759ms` | `110.398ms` | `115.573ms` | `119.293ms` |
| `gic_enable_detail.legacy_irq_count` | `4` | `4` | `4` | `4` |
| `interrupt_group_update_many.register_irqfd_ms` | `21.179ms` | `80.032ms` | `89.900ms` | `119.414ms` |

复测确认该优化稳定，并且 `130459` 不是每轮必现。

## virtio-pci restore 细分验证

新增诊断：

- `VirtioPciDevice::restore()` 输出 MSI-X、common config、PCI config、queue state、activate 各阶段。
- `VirtioPciDeviceActivator::activate()` 输出底层 device activate、状态写入、barrier 等阶段。

本地 x86 验证：

```bash
cargo fmt --manifest-path hypervisor/Cargo.toml --package virtio-devices
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建和部署：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：约 `4m10s`
- 新 shim sha256：`dfce491c894bb67fc850bc05061836547f83cec53ad95ac0e57b317a428d850f`
- 新 cube-runtime sha256：`e167b907b664989896da65f589dc83daadc2d5acfd4f321cf872c88981307a71`
- 旧 shim 备份：`containerd-shim-cube-rs.bak-20260603142428`

smoke：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py virtio-detail-smoke-c1-n1 1 1
```

结果：

- create p99：`32.5ms`
- 清理：`1/1`

c100：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py virtio-detail-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c100 n200 | `99.5%` | `884ms` | `199/199` |

1 个失败为此前已见过的 `130459` 初始化失败。

按本 case 的 199 个成功 sandbox id 过滤 VMM 日志后：

| 阶段 | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| `vmm_vm_restore.total_ms` | `439.591ms` | `693.403ms` | `742.011ms` | `777.572ms` |
| `vm_restore.restore_devices_ms` | `405.051ms` | `642.568ms` | `701.086ms` | `727.087ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `18.231ms` | `110.710ms` | `129.779ms` | `212.534ms` |
| `virtio_pci_restore_detail.total_ms` | `99.525ms` | `331.036ms` | `393.511ms` | `461.832ms` |
| `virtio_pci_restore_detail.msix_restore_ms` | `99.415ms` | `330.838ms` | `393.336ms` | `461.599ms` |
| `virtio_pci_restore_detail.activate_ms` | `0.114ms` | `0.225ms` | `0.339ms` | `0.919ms` |
| `virtio_pci_activate.device_activate_ms` | `0.108ms` | `0.219ms` | `0.334ms` | `0.915ms` |

`virtio_pci_restore_detail` 按设备 p99：

| 设备 | total p99 | MSI-X restore p99 |
| --- | ---: | ---: |
| `_virtio-pci-pmem-cubebox-image-0` | `420.914ms` | `420.808ms` |
| `_virtio-pci-tap-0` | `402.940ms` | `402.786ms` |
| `_virtio-pci-disk-0` | `396.534ms` | `396.347ms` |
| `_virtio-pci-cube-fs` | `394.694ms` | `394.489ms` |
| `_virtio-pci-__rng` | `372.932ms` | `372.814ms` |
| `_virtio-pci-_pmem0` | `372.580ms` | `372.480ms` |
| `_virtio-pci-__console` | `372.472ms` | `372.368ms` |

判断：

- `_virtio-pci-*` restore 的剩余长尾几乎全部来自 `MsixConfig::restore()` / `MsixConfig::set_state()`。
- common config、PCI config、queue state、底层 virtio device `activate()` 都是亚毫秒级，不是下一步优化对象。
- 继续优化应聚焦 MSI-X restore 中的 irqfd 注册次数和并发行为，而不是设备 activate 或 Tokio runtime。

## update_many 锁范围缩小验证

尝试优化：

- `MsiInterruptGroup::update_many()` 原先在函数开头获取共享 `gsi_msi_routes` mutex，并在每个 vector 的 `route.enable()` / `register_irqfd` 完成后才释放。
- 调整为先完成每个 vector 的 enable/disable，并把 `(gsi, RoutingEntry)` 暂存在本地 `Vec`。
- 最后只在插入共享 route map 和 `set_gsi_routes()` 时持有 `gsi_msi_routes` mutex。

目的：

- 减少同一个 VM 内多个 `_virtio-pci-*` 设备在 MSI-X restore 时被共享 route map 锁串行化。

本地 x86 验证：

```bash
cargo fmt --manifest-path hypervisor/Cargo.toml --package vmm
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建和部署：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：约 `4m01s`
- 新 shim sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
- 新 cube-runtime sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 旧 shim 备份：`containerd-shim-cube-rs.bak-20260603143239`

smoke：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py update-many-lock-smoke-c1-n1 1 1
```

结果：

- create p99：`37.1ms`
- 清理：`1/1`

c100：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py update-many-lock-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 清理 |
| --- | ---: | ---: | ---: |
| c100 n200 | `100%` | `805.4ms` | `200/200` |

按本 case 的 200 个 sandbox id 过滤 VMM 日志后：

| 阶段 | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| `vmm_vm_restore.total_ms` | `437.066ms` | `694.696ms` | `713.231ms` | `760.632ms` |
| `vm_restore.restore_devices_ms` | `339.688ms` | `573.897ms` | `611.847ms` | `662.781ms` |
| `vgic_restore_detail.enable_interrupt_ms` | `45.753ms` | `204.453ms` | `224.093ms` | `237.930ms` |
| `virtio_pci_restore_detail.msix_restore_ms` | `128.543ms` | `323.827ms` | `388.127ms` | `451.779ms` |
| `interrupt_group_update_many.register_irqfd_ms` | `56.141ms` | `172.484ms` | `202.520ms` | `235.884ms` |
| `interrupt_group_update_many.set_gsi_routes_ms` | `36.289ms` | `173.217ms` | `218.386ms` | `329.668ms` |

判断：

- 端到端 p99 从上一轮 `virtio-detail-c100-n200` 的约 `884ms` 降到约 `805ms`。
- `restore_devices_ms` p99 从约 `701ms` 降到约 `612ms`，说明缩小锁范围对设备 restore 有正向效果。
- 但 `set_gsi_routes_ms` 由原先约 `0.1ms` 级变成 p99 约 `218ms`，说明并发刷新同一个 VM 的 GSI routing map 也会产生明显内核/共享结构开销。
- 该优化是正向但仍需谨慎保留：它减少了共享 mutex 对 `register_irqfd` 的串行化，但把下一层瓶颈暴露为并发 `set_gsi_routing`。
- 后续更理想的方向是 restore 阶段合并同一 VM 内多个 MSI-X group 的 route update，只在设备 restore 批次末尾调用少量 `set_gsi_routing`；但这需要更大的接口设计，不适合在当前轮次继续快速改动。

## MSI-X set_state 去掉全量 enable 负向验证

尝试优化：

- `MsixConfig::set_state()` 在 restore 时先收集未 mask vector，并调用 `interrupt_source_group.update_many(&configs)`。
- `update_many()` 已会对未 mask vector 执行 `route.enable()` / `register_irqfd`。
- 因此尝试移除 `update_many()` 后面的全量 `interrupt_source_group.enable()`，希望减少对整个 MSI-X group 的重复遍历和可能的提前 irqfd 注册。

本地 x86 验证：

```bash
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建：

- 初次构建遇到远端 GitHub 访问超时，`cargo` 卡在 `https://github.com/rust-vmm/vm-fdt`。
- 按既定策略，从本地 Cargo git cache 打包并推送了缺失依赖 cache：
  - `vm-fdt-15a5500c6de3ef67`
  - `mshv-a0c7b8353999776c`
  - `vfio-2d61e71a0a55b0af`
  - `vhost-e6137da7836efc78`
  - `micro-http-b6958a74e1f08106`
- 补齐后，远端可正常完成 `CubeShim` release 构建。

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py msix-no-enable-smoke-c1-n1 1 1
./run_create_only_case.py msix-no-enable-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create p99 | 错误数 | 清理 |
| --- | ---: | ---: | ---: | ---: |
| c1 n1 smoke | `100%` | `34.336ms` | `0` | `1/1` |
| c100 n200 | `98%` | `931.550ms` | `4` | `196/196` |

c100 错误类型：

- `CubeMaster returned error code 130459: Failed to initialize the container`
- `failed to start shim: start failed: failed to create TTRPC connection`

判断：

- 该优化不保留。
- smoke 可以通过，但 c100 下成功率从上一轮稳定结果的 `100%` 退化到 `98%`，并重新出现 shim TTRPC 连接失败。
- `set_state()` 后的全量 `enable()` 虽然看起来有重复注册成本，但它仍可能承担了恢复后保证整个 MSI-X group irqfd 注册完整性的作用；直接移除会放大高并发初始化失败风险。
- 已将本地和远端代码恢复到上一版稳定实现。

恢复验证：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restore-stable-smoke-c1-n1 1 1
```

结果：

- create p99：`30.163ms`
- 成功率：`100%`
- 清理：`1/1`
- 清理后 sandbox 数：`0`

恢复后的远端运行时二进制：

- `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
- `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 原子替换备份：`20260603150256`

## restore 阶段延迟 GSI route flush 负向验证

尝试优化：

- 在 `InterruptManager` trait 上增加 restore 阶段 route update 延迟刷新钩子。
- `MsiInterruptGroup::update()` / `update_many()` 在延迟段只更新共享 route map 并标记 dirty。
- `DeviceManager::restore_devices()` 包裹设备 restore 批次，结束时由 `MsiInterruptManager` 统一调用一次 `set_gsi_routing`。
- 目标是减少同一个 VM 内多个 MSI-X group 并发调用 `set_gsi_routing` 的内核开销。

本地 x86 验证：

```bash
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release`
- 构建耗时：`4m06s`
- 临时部署 shim sha256：`01ec0f5b485bd256ed614afe2eb9b8088244a2edc65709ccf93f4b7039598172`
- 临时部署 cube-runtime sha256：`639bb9949a5c0fdc23ad52bf728e61e96391340e10ae8ca2bc8e4c8f7daeaec3`
- 回滚备份：`20260603155055`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py deferred-routes-smoke-c1-n1 1 1
./run_create_only_case.py deferred-routes-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create avg | create p50 | create p95 | create p99 | total_time / 备注 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| c1 n1 smoke | `100%` | `31.8ms` | `31.8ms` | `31.8ms` | `31.8ms` | 清理 `1/1` |
| c100 n200 | `99.5%` | `464.098ms` | `547.733ms` | `856.977ms` | `902.983ms` | 1 个 shim TTRPC 连接失败 |

c100 错误：

- `failed to start shim: start failed: failed to create TTRPC connection`
- 残留异常 shim：`containerd-shim-cube-rs -id 5f3d7fb0c7984df78b82cecc21aab26e`

VMM 日志观察：

- 每个 `interrupt_group_update_many` 内 `set_gsi_routes_ms` 降到约 `0.001ms`，`deferred_route_update=true`。
- 批次末尾 `msi_deferred_route_flush.total_ms` 约 `0.07-0.09ms`，`route_count=31`。
- 但 `register_irqfd_ms` 仍在高并发下出现百毫秒级长尾；示例中单组 `register_irqfd_ms` 可达 `167ms`、`201ms`、`230ms`。

判断：

- 该优化不保留。
- 延迟合并 `set_gsi_routing` 本身生效，但当前 c100 主体瓶颈已经转向并发 `register_irqfd` 和 ARM64 VGIC ioctl 排队。
- 在当前 restore 启动时序中延迟 route flush 还引入可靠性风险，c100 成功率从稳定版本的 `100%` 降到 `99.5%`，且产生残留 shim。
- 后续优化不应继续沿“只合并 GSI route flush”方向推进，应转向减少/限流 irqfd 注册风暴，或控制 VMM restore 内设备/GIC ioctl 并发度。

恢复操作：

```bash
kill -9 3349924
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py post-rollback-smoke-c1-n1 1 1
```

恢复结果：

- 远端 runtime 已恢复到稳定版本：
  - `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
  - `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 回滚 smoke：create p99 `32.1ms`，成功率 `100%`，清理 `1/1`。
- 本地 deferred route flush 代码已撤回，仅保留本段验证记录。

## restore device worker 降到 2 负向验证

尝试优化：

- 保持 snapshot 等其他路径的 `MAX_WORKER_THREADS=5` 不变。
- 仅在 `DeviceManager::restore_device_node()` 引入临时 `MAX_RESTORE_WORKER_THREADS=2`。
- 目标是降低单 VM 内 `_virtio-pci-*` 设备 restore 的并发度，观察是否能减少 c100 下 `register_irqfd` / `set_gsi_routing` / VGIC ioctl 的全局排队。

本地 x86 验证：

```bash
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release`
- 构建耗时：`4m02s`
- 临时部署 shim sha256：`3993409f02e87013a0988930e76a630fdc25b0d21ce0aa6a43d73d74bbc096ad`
- 临时部署 cube-runtime sha256：`cdc0d2b43a14b1e07ecae325f0f1db8926dcae9c6b47c8c55d06fd252a94b510`
- 回滚备份：`20260603160216`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restore-worker2-smoke-c1-n1 1 1
./run_create_only_case.py restore-worker2-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create avg | create p50 | create p95 | create p99 | 备注 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| c1 n1 smoke | `100%` | `33.2ms` | `33.2ms` | `33.2ms` | `33.2ms` | 清理 `1/1` |
| c100 n200 | `99.5%` | `482.488ms` | `533.546ms` | `871.365ms` | `953.000ms` | 1 个 `130459` 初始化失败 |

日志观察：

- `device_restore_node_total.worker_threads=2` 已确认生效。
- 部分样本 `restore_devices_ms` 仍在 `390-466ms`，单 VM 内设备 restore 串行化增加了单 VM 的 restore wall time。
- `interrupt_group_update_many.register_irqfd_ms` 仍可出现 `140ms`、`171ms`、`182ms` 等长尾。
- `set_gsi_routes_ms` 仍可出现 `80ms`、`90ms`、`129ms` 等长尾。
- `vgic_restore_detail.enable_interrupt_ms` 仍在 `150-196ms` 区间。

判断：

- 该优化不保留。
- 将每个 VM 内 restore worker 从 5 降到 2 没有消除全局 ioctl 排队，反而增加了单 VM 内设备 restore 的串行时间。
- c100 成功率从稳定版本 `100%` 下降到 `99.5%`，p99 从稳定版本约 `805ms` 上升到约 `953ms`。
- 后续不应继续粗粒度降低每 VM restore worker；如果要做限流，应改为更有针对性的 ioctl/irqfd 注册限流或全局并发闸门，而不是压低所有设备 restore 并发。

恢复结果：

- 远端 runtime 已恢复到稳定版本：
  - `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
  - `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 本地 worker=2 临时代码已撤回，仅保留本段验证记录。

## ARM64 GIC enable 延后到设备 restore 后负向验证

尝试优化：

- 将 `Vm::restore()` 中 ARM64 vGIC restore 拆成两段。
- 第一段仍在 `restore_devices()` 前完成 GIC 创建、PMU 初始化、GICR typer 设置和 GIC state restore。
- 第二段将 `Gic::enable()` 延后到 `restore_devices()` 后、`start_restored_vcpus()` 前执行。
- 目标是错开 c100 下早期 GIC legacy IRQ routing ioctl 和设备 MSI-X irqfd/GSI routing ioctl 的拥塞峰值。

本地 x86 验证：

```bash
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release`
- 构建耗时：`4m01s`
- 临时部署 shim sha256：`4cfb95055b6546fc3ced5da19256089fb63b366f5da689fe97c05eecc0051fe4`
- 临时部署 cube-runtime sha256：`fe2eebafb6f9b571e9a857822cc80c6cba94b9ea3f92bcf966394bc06c5c3bce`
- 回滚备份：`20260603161315`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py gic-enable-after-devices-smoke-c1-n1 1 1
./run_create_only_case.py gic-enable-after-devices-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create avg | create p50 | create p95 | create p99 | 备注 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| c1 n1 smoke | `100%` | `33.4ms` | `33.4ms` | `33.4ms` | `33.4ms` | 清理 `1/1` |
| c100 n200 | `100%` | `508.023ms` | `541.835ms` | `909.833ms` | `959.489ms` | 清理 `200/200` |

日志观察：

- `vgic_restore_state_detail.total_ms` 降到约 `0.5ms`，说明 GIC create/state restore 本身很轻。
- 延后的 `vgic_enable_interrupt_detail.total_ms` 多数约 `20-45ms`，单独看并不大。
- 但设备 restore 阶段的 MSI-X `interrupt_group_update_many` 长尾明显放大：
  - `set_gsi_routes_ms` 可达约 `80-133ms`。
  - `register_irqfd_ms` 可达约 `180-230ms`。
  - 单个 `update_many.total_ms` 可达约 `300ms+`。
- `restore_devices_ms` 在后段样本中仍约 `360-424ms`，整体 create p99 退化到约 `959ms`。

判断：

- 该优化不保留。
- 延后 GIC enable 可以降低 `vgic_restore_ms` 的账面值，但没有降低端到端尾延迟；设备 MSI-X route/irqfd ioctl 反而承担了更明显的长尾。
- 稳定性保持 `100%`，但性能差于稳定基线 c100 p99 约 `805ms`。
- 后续不应继续通过简单调整 GIC enable 与 device restore 的顺序来优化；更需要针对 MSI-X `register_irqfd` / `set_gsi_routing` 本身做数量减少、批次策略或全局 ioctl 限流。

恢复结果：

- 远端 runtime 已恢复到稳定版本：
  - `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
  - `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 本地 GIC enable 延后实验代码已撤回，仅保留本段验证记录。

## restore device worker 提高到 10 负向验证

尝试优化：

- 保持 snapshot 等其他路径语义不变。
- 仅在 `DeviceManager::restore_device_node()` 引入临时 `MAX_RESTORE_WORKER_THREADS=10`。
- 目标是提高单 VM 内 restore device 并行度，观察是否能缩短 `restore_devices_ms`，并摊薄 c100 下的 create-only 尾延迟。

本地 x86 验证：

```bash
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release`
- 临时部署 shim sha256：`7ff94c26fec987f7cabecac756ea91b345c36754cbf4ac8a1528154d31266887`
- 临时部署 cube-runtime sha256：`438cca898226f9292a66027a3b9033a9b2bc28af44c0f57499069a62e68ed4fc`
- 回滚备份：`20260603162158`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restore-worker10-smoke-c1-n1 1 1
./run_create_only_case.py restore-worker10-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create avg | create p50 | create p90 | create p95 | create p99 | 备注 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| c1 n1 smoke | `100%` | `31.4ms` | `31.4ms` | `31.4ms` | `31.4ms` | `31.4ms` | 清理 `1/1` |
| c100 n200 | `100%` | `480.320ms` | `521.046ms` | `871.617ms` | `916.121ms` | `976.629ms` | 清理 `200/200` |

日志观察：

- `device_restore_node_total.worker_threads=10` 已确认生效。
- 部分普通样本的 `restore_devices_ms` 可以降到约 `68-244ms`，说明更高 worker 数确实可能缩短部分 VM 的设备 restore wall time。
- 但尾部样本中的 KVM ioctl 竞争显著放大：
  - `interrupt_group_update_many.register_irqfd_ms` 最高约 `475.001ms`。
  - `interrupt_group_update_many.set_gsi_routes_ms` 最高约 `300.445ms`。
  - 单个 `interrupt_group_update_many.total_ms` 最高约 `576ms`。
  - 尾部样本出现 `restore_devices_ms=590.115ms`、`vgic_restore_ms=271.390ms`、`vmm_vm_restore=866.020ms`。

判断：

- 该优化不保留。
- c100 成功率恢复到 `100%`，但 p99 从稳定版本约 `805ms` 上升到约 `977ms`，端到端尾延迟明显退化。
- 提高每 VM 内 restore worker 会把更多 `register_irqfd` / `set_gsi_routing` 并发压到同一批 KVM ioctl 上，局部缩短普通样本，整体放大 p95/p99。
- 后续不应继续粗粒度提高单 VM 内设备 restore 并发；更合理的方向是围绕 MSI-X route/irqfd 数量、批次、或跨 VM 的全局 ioctl 限流做细粒度控制。

恢复结果：

- 远端 runtime 已恢复到稳定版本：
  - `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
  - `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 本地 worker=10 临时代码已撤回，仅保留本段验证记录。

## ARM64 IRQ routing 进程间文件锁限流负向验证

尝试优化：

- 仅在 ARM64 下，为 `register_irqfd` / `unregister_irqfd` / `set_gsi_routing` 增加进程间文件锁限流。
- 初始限流槽数为 `8`，锁文件路径形如 `/tmp/cubesandbox-kvm-irq-routing-<slot>.lock`。
- 目标是将 c100 下跨 shim 进程的 KVM IRQ routing ioctl 风暴压到有限并发，验证是否能降低 `register_irqfd_ms` / `set_gsi_routes_ms` 的尾延迟。

本地 x86 验证：

```bash
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。该实验代码在 x86 下为 no-op。

远端 ARM64 构建与部署：

- 第一次构建误复用了远端源码中残留的 `MAX_RESTORE_WORKER_THREADS=10`，因此 `irq-lock8-c100-n200` 是 `worker=10 + lock8` 混合数据，不作为纯限流结论。
- 随后重新同步本地稳定 `device_manager.rs`，确认远端源码仅保留 `MAX_WORKER_THREADS=5`。
- 纯 `lock8 + worker=5` 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release`
- 纯实验构建耗时：`4m04s`
- 纯实验 shim sha256：`159e074cb5dd681b989286158a9fc3146351d747ac8389a925c3bc709a3812b8`
- 纯实验 cube-runtime sha256：`5963771c898124924071262238e215f2bacb0339068e503788c5f092eeebb1e8`
- 回滚备份：`202606031708_pure_irqlock8`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py pure-irq-lock8-smoke-c1-n1 1 1
./run_create_only_case.py pure-irq-lock8-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create avg | create p50 | create p90 | create p95 | create p99 | 总耗时 | 备注 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| c1 n1 smoke | `100%` | `32.7ms` | `32.7ms` | `32.7ms` | `32.7ms` | `32.7ms` | `0.0328s` | 清理 `1/1` |
| c100 n200 | `100%` | `536.8ms` | `538.2ms` | `1042.7ms` | `1079.3ms` | `1171.1ms` | `1.2497s` | 清理 `200/200` |

对比稳定基线：

- 稳定基线 c100 n200：成功率 `100%`，avg 约 `489.1ms`，p50 约 `558.5ms`，p95 约 `762.4ms`，p99 约 `805.4ms`，总耗时约 `1.203s`。
- 纯 `lock8` 后：成功率仍为 `100%`，但 p95 上升到约 `1079ms`，p99 上升到约 `1171ms`，总耗时也从约 `1.203s` 上升到约 `1.250s`。

日志观察：

- 纯实验日志确认 `device_restore_node_total.worker_threads=5`。
- 大量 `interrupt_group_update_many` 中，`irq_routing_lock_wait_ms` 几乎等于 `register_irqfd_ms` 的大部分耗时。
- 典型尾部样本：
  - `total_ms=777.867ms`，`register_irqfd_ms=772.602ms`，`irq_routing_lock_wait_ms=771.334ms`。
  - `total_ms=647.613ms`，`register_irqfd_ms=609.257ms`，`irq_routing_lock_wait_ms=603.025ms`。
  - `total_ms=593.566ms`，`register_irqfd_ms=592.747ms`，`irq_routing_lock_wait_ms=591.459ms`。
- 这说明文件锁把原先内核 ioctl 排队显式转移成用户态锁等待，但没有降低端到端尾延迟。

判断：

- 该优化不保留。
- 粗粒度进程间文件锁限流会牺牲 p95/p99，无法改善 create-only 端到端性能。
- 后续不应继续在用户态对所有 IRQ routing ioctl 做统一限流；更合理的方向是减少 MSI-X route/irqfd 注册数量，或者在设备/队列层面避免不必要的恢复期注册。

恢复结果：

- 远端 runtime 已恢复到稳定版本：
  - `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
  - `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 本地 IRQ routing 文件锁限流实验代码已撤回，仅保留本段验证记录。

## MSI-X restore 去掉后置 enable 负向验证

尝试优化：

- 在 `MsixConfig::set_state()` 中保留 `interrupt_source_group.update_many(&configs)`。
- 临时去掉后续的 `interrupt_source_group.enable()`。
- 背景是日志显示 `update_many()` 已经注册未 masked vectors，而后续 `enable()` 大多数是 no-op，但少数场景会额外注册 1 个 route 并产生几十到 200ms 左右的 `register_irqfd_ms`。
- 目标是减少 restore 阶段不必要的 irqfd 注册，降低 `interrupt_group_enable` 的尾部额外开销。

本地 x86 验证：

```bash
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。该临时改动会让 `EnableInterruptRoute` enum 变体变为未使用。

远端 ARM64 构建：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release`
- 构建耗时：`4m09s`
- 临时部署 shim sha256：`2e4ea98e416135d6eba7521b627e713e84645c2430ebf9bb1f4c6166acec0a81`
- 临时部署 cube-runtime sha256：`7e686e9911e8d2c2422fea580267c482233db4fe8851ee61ab243773f229a835`
- 回滚备份：`202606031728_msix_no_enable`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py msix-no-enable-smoke-c1-n1 1 1
./run_create_only_case.py msix-no-enable-c100-n200 100 200
```

结果：

| 场景 | 成功率 | create avg | create p50 | create p95 | create p99 | 总耗时 | 备注 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| c1 n1 smoke | `100%` | `32.4ms` | `32.4ms` | `32.4ms` | `32.4ms` | `0.0325s` | 清理 `1/1` |
| c100 n200 | `99.0%` | 未保留 | 未保留 | 未保留 | `993ms` | `1.1371s` | 2 个 `130459` 初始化失败，清理 `198/198` |

日志观察：

- `interrupt_group_enable` 的后置 no-op / 额外注册基本消失，说明改动确实移除了那段额外路径。
- 但 `interrupt_group_update_many` 本身仍出现 600ms 级长尾：
  - `total_ms=701.859ms`，`register_irqfd_ms=543.697ms`，`set_gsi_routes_ms=158.157ms`。
  - `total_ms=625.496ms`，`register_irqfd_ms=442.424ms`，`set_gsi_routes_ms=183.068ms`。
  - `total_ms=615.585ms`，`register_irqfd_ms=615.129ms`。
- c100 结果出现 2 个初始化失败，说明该语义调整存在恢复完整性或时序风险。

判断：

- 该优化不保留。
- 去掉 `enable()` 可以消除一个小的额外注册来源，但不能解决主瓶颈：`update_many()` 内对实际未 masked vectors 的 irqfd 注册和 GSI route 更新。
- 成功率从稳定基线 `100%` 降到 `99%`，p99 也高于稳定基线约 `805ms`，因此不适合作为保留优化。
- 后续应继续聚焦 `update_many()` 内部的 route 数量和设备恢复时机，而不是删除 restore 后的保险性 `enable()`。

恢复结果：

- 远端 runtime 已恢复到稳定版本：
  - `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
  - `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 本地和远端 `msix.rs` 临时代码均已撤回，仅保留本段验证记录。

## Restore 级 GSI route batch flush 负向验证

背景：

- 先前的 route cache 锁范围缩短实验显示：`route_cache_lock_hold_ms` 可以被压低，但 GSI routing flush 本身仍会形成串行热点。
- 因此进一步尝试 restore 级 batch：在单 VM restore 期间推迟多次 `KVM_SET_GSI_ROUTING`，等设备 restore 结束后统一 flush。
- 目标是减少 `KVM_SET_GSI_ROUTING` 次数，降低 restore 并发下的长尾。

本地 x86 验证：

```bash
cargo fmt --manifest-path hypervisor/Cargo.toml -- --check
cargo check --manifest-path hypervisor/Cargo.toml -p vmm --features kvm
```

结果：通过，仅有项目既有 warning。

远端 ARM64 构建与部署：

- 构建目录：`/opt/cubesandbox-build/upper-create-timing-20260602-src`
- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release`
- 构建耗时：约 `4m06s`
- 临时部署 shim sha256：`75845f8ecdfc44cd30ad9fc0d52a6dbe50f5522b6cb0719e5205ef09b1fa4e16`
- 临时部署 cube-runtime sha256：`7be31736ffc3f21ab39d6eed652fb4df116add3c389c35191348ff5466b56ffc`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py msi-route-batch-smoke-c1-n1 1 1
./run_create_only_case.py msi-route-batch-c50-n100 50 100
./run_create_only_case.py msi-route-batch-c100-n200 100 200
```

原始数据已保存到本地：

```text
.trellis/tasks/05-06-arm64-ci-release/research/tap-fd-timeout-10s-20260602/results/20260604-msix-route-experiments/
```

结果：

| 场景 | 成功率 | errors | create avg | create p50 | create p90 | create p95 | create p99 | create max | e2e 总耗时 | 吞吐 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| c1 n1 smoke | `100%` | `0` | `52.188ms` | `52.188ms` | `52.188ms` | `52.188ms` | `52.188ms` | `52.188ms` | `0.0524s` | `19.09/s` |
| c50 n100 | `99.0%` | `1` | `146.045ms` | `154.447ms` | `196.270ms` | `204.668ms` | `218.712ms` | `218.712ms` | `0.3670s` | `269.77/s` |
| c100 n200 | `99.5%` | `1` | `452.813ms` | `528.842ms` | `779.715ms` | `847.993ms` | `895.409ms` | `935.491ms` | `1.1210s` | `177.53/s` |

对比稳定基线：

- 稳定基线 c100 n200：成功率 `100%`，p99 约 `805.4ms`，e2e 总耗时约 `1.203s`。
- batch flush c100 n200：成功率降到 `99.5%`，p99 升到 `895.4ms`。虽然 e2e 总耗时略低，但不稳定且尾延迟变差。

判断：

- 该优化不保留。
- 二进制中确认存在 `interrupt_manager_route_batch`、`deferred_gsi_routes` marker，说明测试二进制确实包含本次实验代码。
- cubelet 捕获日志未能稳定收集到 shim 内部 timing，因此本次主要依据 create-only 原始结果和二进制 marker。
- 结果说明单纯推迟 GSI routing flush 不足以解决主要长尾，且会引入恢复时序风险。

恢复结果：

- 远端 runtime 已恢复到稳定版本：
  - `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
  - `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 本地 batch flush 实验代码已撤回；当前仅保留已验证有效的观测字段和 MSI-X restore 后置全量 `enable()` 删除。

## MSI-X selected-vector restore 负向验证

背景：

- `MsixConfig::set_state()` 在 restore 时会遍历 MSI-X table 中所有未 masked entries，并通过 `update_many()` 注册 irqfd 和刷新 GSI route。
- 实验尝试只恢复 virtio common config 中实际被 driver 选择的 config vector 和 queue vectors，减少 restore 阶段 MSI-X route/irqfd 注册数量。

实验过程：

1. v1 直接从父 snapshot 读取 `virtio_pci_common_config` 状态，smoke 失败。
2. v2 改为先进入 common config 子 snapshot，再读取 selected vectors；若子 snapshot 不存在则回退到原始全量 MSI-X restore。

v1 失败原因：

- `snapshot.to_state(common_config.id())` 被错误地用于父 snapshot。
- smoke `0/1`，错误为缺少 `virtio_pci_common_config` section。

远端 ARM64 v2 构建与部署：

- 临时部署 shim sha256：`b1c6c2e86d2a2939924d57d72ea18a73720c292fb5b555e5781cbb94a8ee6103`
- 临时部署 cube-runtime sha256：`62ed4cbd109ddbb4c8c4bcf197f5e40189904e2cb63f64730908d7a029489d92`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py msix-selected-v2-smoke-c1-n1 1 1
./run_create_only_case.py msix-selected-v2-smoke2-c1-n1 1 1
./run_create_only_case.py msix-selected-v2-c50-n100 50 100
./run_create_only_case.py msix-selected-v2-c100-n200 100 200
```

结果：

| 场景 | 成功率 | errors | create avg | create p50 | create p90 | create p95 | create p99 | create max | e2e 总耗时 | 吞吐 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| v2 c1 n1 smoke | `100%` | `0` | `405.791ms` | `405.791ms` | `405.791ms` | `405.791ms` | `405.791ms` | `405.791ms` | `0.4062s` | `2.46/s` |
| v2 c1 n1 smoke2 | `100%` | `0` | `35.621ms` | `35.621ms` | `35.621ms` | `35.621ms` | `35.621ms` | `35.621ms` | `0.0359s` | `27.89/s` |
| v2 c50 n100 | `99.0%` | `1` | `105.861ms` | `104.739ms` | `147.658ms` | `151.885ms` | `167.518ms` | `167.518ms` | `0.2657s` | `372.60/s` |
| v2 c100 n200 | `100%` | `0` | `446.663ms` | `515.778ms` | `758.943ms` | `777.805ms` | `830.833ms` | `874.825ms` | `1.0615s` | `188.41/s` |

对比稳定基线：

- 稳定基线 c100 n200：成功率 `100%`，p99 约 `805.4ms`，e2e 总耗时约 `1.203s`。
- selected-vector v2 c100 n200：成功率 `100%`，e2e 总耗时降到 `1.0615s`，但 p99 升到 `830.8ms`。
- c50 n100 出现 `1` 个 `130459` 初始化失败。

判断：

- 该优化不保留。
- selected-vector 思路能够降低部分中位和总耗时，但没有改善 c100 p99，并且 c50 出现初始化失败。
- 语义风险较高：restore 时仅依据 common config selected vector 恢复 MSI-X，可能遗漏设备恢复或 guest 后续路径依赖的 route 状态。
- 后续不应继续在 `MsixConfig::restore()` 内跳过未 selected entries，除非先有更完整的 virtio/PCI MSI-X 状态机证明和失败复现收敛。

恢复结果：

- 远端 runtime 已恢复到稳定版本：
  - `containerd-shim-cube-rs` sha256：`1e18ca7000faa9dfdebc07f93f35c71372f9ec716e2d1adb8c6e6797e94a96fc`
  - `cube-runtime` sha256：`36c270319394f5712d057d6485667abd7b8f8cfcea1380f2468467748d604320`
- 本地 selected-vector 实验代码已撤回。

## 当前转向判断

截至 2026-06-04，已经确认不适合继续推进的方向：

- 粗粒度 IRQ routing 文件锁限流：会显著拉高 p95/p99。
- route cache 锁范围收缩：会把等待转移到 flush 顺序路径，未形成稳定收益。
- restore 级 GSI route batch flush：p99 变差且出现初始化失败。
- MSI-X selected-vector restore：语义风险高，p99 未改善，c50 出现初始化失败。

下一步应转向更保守的定位和优化：

1. 保留 `MsiInterruptGroup::update_many()` 内部观测字段，以及 MSI-X restore 后置全量 `enable()` 删除。
2. 用日志先确认每类 virtio 设备在 restore 中实际注册的 MSI-X vector 数量、queue 数量、route 数量。
3. 优先寻找“为什么 snapshot 模板里存在这么多需要立即恢复的 MSI-X entries”，而不是在 restore 末端强行跳过注册。
4. 若要减少注册数量，应先从模板生成、设备配置、virtqueue/MSI-X vector 分配策略入手，保证语义等价后再改 restore。

## 2026-06-04 转向后修正

最新验证已经修正上述转向判断中的两点：

- MSI-X restore 后置全量 `enable()` 删除已经作为当前保留优化。`update_many()` 已经启用本次未 masked vector，后置全量 `enable()` 会重复扫描整个 interrupt group。
- route cache 锁范围收缩、restore 级 batch flush、selected-vector 跳过、`irqfd-after-routing` 顺序调整均不保留。

新增状态画像确认：

- 单沙箱模板中所有未 masked MSI-X entries 都已经被 virtio common config 选中。
- `msix_unmasked_unselected_entries=0`，因此 selected-vector 层面没有安全跳过空间。
- 当前需要恢复的 MSI-X vector 数主要来自模板设备和 queue 数量，而不是 restore 层产生的“多余未选中 entry”。

`irqfd-after-routing` 顺序实验结果：

| 场景 | 成功率 | errors | create avg | create p99 | e2e 总耗时 | 吞吐 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| c1 n1 | `100%` | `0` | `48.766ms` | `48.766ms` | `0.0489s` | `20.44/s` |
| c50 n100 | `98.0%` | `2` | `154.799ms` | `228.933ms` | `0.3967s` | `247.03/s` |

结论：

- `irqfd-after-routing` 降低了 `register_irqfd_ms` 的统计值，但把等待转移到了 `route_cache_lock_wait_ms` / `set_gsi_routes_ms`，并且 c50 成功率退化到 `98%`。
- 本地代码已恢复为 `update_many()` 内先注册未 masked route，再统一刷新 GSI routing。
- 后续优先从 benchmark 模板设备/队列/vector 数量和 KVM IRQ routing 并发容量继续分析。

远端已恢复到已验证部署：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：`4m05s`
- shim sha256：`a12968836a3b81cdb7d633a0c08e364f918265685bd5c02d5d12d27ad50d93bd`
- cube-runtime sha256：`1916edb0c04fdf5e139660c5b0ef46dbbf0dde6aa2ef936bb3655429484c3a83`
- `containerd.service` / `cube-sandbox-cubelet.service`：均为 `active`
- profile systemd drop-in：已清理
- 当前沙箱列表：`[]`

恢复后 smoke：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restored-skip-enable-smoke-c1-n1 1 1
```

结果：`100%` 成功，create p99 `45.9ms`，清理 `1/1`。

最新原始数据目录：

```text
.trellis/tasks/05-06-arm64-ci-release/research/tap-fd-timeout-10s-20260602/results/20260604-restore-profile/
```

## 2026-06-05 no-profile 基线复测

远端当前部署：

- 机器：`root@192.168.25.61`
- shim sha256：`a12968836a3b81cdb7d633a0c08e364f918265685bd5c02d5d12d27ad50d93bd`
- cube-runtime sha256：`1916edb0c04fdf5e139660c5b0ef46dbbf0dde6aa2ef936bb3655429484c3a83`
- profile：关闭
- template：`tpl-arm64-bench-ubuntu2204`
- mode：`create-only`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py restored-skip-enable-noprofile-c50-n100-20260605 50 100
./run_create_only_case.py restored-skip-enable-noprofile-c100-n200-20260605 100 200
./run_create_only_case.py restored-skip-enable-noprofile-c200-n400-20260605 200 400
```

结果：

| 并发 / 总量 | 成功率 | errors | create avg | create p95 | create p99 | e2e 总耗时 | 吞吐 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 50 / 100 | `100%` | `0` | `142.747ms` | `204.364ms` | `208.747ms` | `0.3728s` | `268.25/s` |
| 100 / 200 | `100%` | `0` | `433.137ms` | `734.737ms` | `779.553ms` | `1.0783s` | `185.48/s` |
| 200 / 400 | `100%` | `0` | `1598.163ms` | `3084.151ms` | `3224.967ms` | `4.4933s` | `89.02/s` |

判断：

- 当前恢复部署三档均 `100%` 成功，清理后沙箱数为 `0`。
- c100 p99 `779.553ms`，可作为当前 no-profile 对比基线。
- c200 p99 `3224.967ms`，说明高并发 restore 容量瓶颈仍然存在。
- 下一步建议在该基线下验证 benchmark 模板设备/队列/vector 减量，或评估 create 并发闸门。

## 2026-06-05 release candidate 验证

发布候选代码收敛：

- 保留当前分支历史中已验证的 ARM64/network-agent/CubeVS/one-click 优化。
- 保留 MSI-X restore 后置全量 `enable()` 删除。
- 撤回本轮未提交的 restore profile 画像/日志字段，避免 release 包携带临时诊断噪声。

远端部署：

- 构建命令：`cd /opt/cubesandbox-build/upper-create-timing-20260602-src/CubeShim && cargo build --release --locked`
- 构建耗时：`4m05s`
- shim sha256：`c43da63c6e50e67297b32fa03bf2a02c6fdaf940d7f217e35504d4099566c3bd`
- cube-runtime sha256：`b4df7ea4e633cbf2a2da3cc22a6183b6800f5c0cf6c2b99b3b45948bbf85ccd8`
- profile：关闭
- 服务状态：`containerd` / `cube-sandbox-cubelet` 均为 `active`

测试命令：

```bash
cd /opt/cubesandbox-benchmarks/upper-create-timing-20260602
./run_create_only_case.py release-candidate-msix-skip-smoke-c1-n1-20260605 1 1
./run_create_only_case.py release-candidate-msix-skip-c100-n200-20260605 100 200
./run_create_only_case.py release-candidate-msix-skip-c100-n200-rerun-20260605 100 200
```

结果：

| 场景 | 成功率 | errors | create avg | create p95 | create p99 | e2e 总耗时 | 吞吐 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| c1 / n1 smoke | `100%` | `0` | `45.806ms` | `45.806ms` | `45.806ms` | `0.0460s` | `21.75/s` |
| c100 / n200 | `98.5%` | `3` | `427.495ms` | `704.523ms` | `764.037ms` | `1.1025s` | `178.68/s` |
| c100 / n200 rerun | `100%` | `0` | `436.120ms` | `755.973ms` | `813.484ms` | `1.0716s` | `186.64/s` |

判断：

- 发布候选 smoke 正常。
- c100 首轮出现 `3` 个既有模式的 `130459` 初始化失败，清理后沙箱数为 `0`；远端服务、磁盘和内存均正常。
- c100 rerun `200/200` 成功，p99 `813.484ms`，与当前 no-profile 基线同一量级。
- 可继续作为优化版 one-click release candidate，但 release notes 需要记录 c100 首轮曾出现偶发 `130459`，高并发容量瓶颈仍待后续模板/vector 减量或并发闸门优化。
