# one-click ARM64 远端部署验证记录（a6f78b8）

## 基本信息

- 验证日期：2026-05-30
- 本地工作区：`/home/lyq/Projects/Micro-VM/CubeSandbox`
- 验证提交：`a6f78b8 Merge branch 'feature/arm64-adaptation'`
- 远端机器：`root@192.168.25.61`
- 远端架构：`aarch64`
- 验证目录：`/opt/cubesandbox-oneclick-verify-a6f78b8`

## 构建结果

基于当前最新提交在远端 ARM64 机器完成了 one-click release 包构建，构建结果成功。

构建命令使用的关键环境变量：

```bash
ONE_CLICK_TARGET_ARCH=arm64
ONE_CLICK_DIST_VERSION=a6f78b8-arm64-verify
ONE_CLICK_CUBE_KERNEL_VMLINUX=/usr/local/services/cubetoolbox/cube-kernel-scf-linux-arm64/vmlinux
ONE_CLICK_CUBE_KERNEL_CONFIG=/usr/local/services/cubetoolbox/cube-kernel-scf-linux-arm64/configs/kernel-oc9-arm64.config
```

构建产物：

```text
/opt/cubesandbox-oneclick-verify-a6f78b8/src/deploy/one-click/dist/cube-sandbox-one-click-a6f78b8-arm64-verify.tar.gz
```

产物大小与校验：

```text
size:   268M
sha256: bb7f33675b2e8d5430b2438e3e58548a44ade646090c6b8f14165a10ebba32a0
```

日志位置：

```text
/opt/cubesandbox-oneclick-verify-a6f78b8/logs/build-a6f78b8.log
```

构建期间确认的结果：

- CubeMaster、CubeMasterCLI、Cubelet、cubecli、network-agent 均完成 ARM64 Go 构建。
- CubeAPI 完成 ARM64 Rust 构建，仅有 dead_code 类 warning。
- cube-agent 完成 ARM64 Rust 构建，debug/release logging 测试通过。
- CubeShim/cube-runtime 完成 ARM64 Rust 构建，仅有 unused_mut warning。
- Guest image 基于 `openeuler/openeuler:24.03-lts-sp3` 构建成功。
- one-click runtime layout 打包成功。

## 部署验证结果

当前结论：构建成功，但 one-click systemd 部署路径不能在该 ARM64 远端机器上无人工修正地完整成功。

部署日志：

```text
/opt/cubesandbox-oneclick-verify-a6f78b8/logs/install-a6f78b8.log
/opt/cubesandbox-oneclick-verify-a6f78b8/logs/install-a6f78b8-retry2.log
```

第一次部署使用默认镜像时，MySQL/Redis 等容器拉起失败。远端 Docker 使用到 amd64 镜像，日志出现：

```text
requested image platform linux/amd64 does not match linux/arm64
exec /usr/local/bin/docker-entrypoint.sh: exec format error
```

第二次部署通过 `.env` 覆盖 ARM64 可用镜像后，以下组件可以启动或曾验证为可运行：

- MySQL：`mysql:8.0`
- Redis：`redis:7-alpine`
- CoreDNS：`coredns/coredns:1.14.2`
- WebUI：`openresty/openresty:1.21.4.1-6-alpine-fat`
- CubeAPI
- Cubelet

但 quickcheck 最终失败：

```text
[quickcheck] role=control
[quickcheck] cubemaster=127.0.0.1:18089
[quickcheck] network-agent-health=127.0.0.1:19091
[quickcheck] cube-api-health=127.0.0.1:13000
[quickcheck] check systemd units
[one-click-runtime] ERROR: expected systemd unit not active: cube-sandbox-network-agent.service
```

## 当前阻塞问题

### 1. systemd 默认镜像仍存在 ARM64 兼容性风险

默认部署路径仍可能使用 Tencent 镜像仓库中的 x86_64 镜像或远端已有的 amd64 缓存镜像，在 ARM64 机器上会触发 `exec format error`。

临时规避方式是在 `.env` 中覆盖镜像：

```bash
CUBE_SANDBOX_MYSQL_IMAGE=mysql:8.0
CUBE_SANDBOX_REDIS_IMAGE=redis:7-alpine
CUBE_PROXY_COREDNS_IMAGE=coredns/coredns:1.14.2
WEB_UI_IMAGE=openresty/openresty:1.21.4.1-6-alpine-fat
```

后续需要让 systemd 部署脚本和普通 one-click 脚本一样，根据 `ONE_CLICK_TARGET_ARCH` 选择架构可用的默认镜像。

### 2. network-agent 健康端口没有传入启动参数

本次 `.env` 设置：

```bash
NETWORK_AGENT_HEALTH_ADDR=127.0.0.1:19091
```

但 systemd 启动脚本实际执行：

```bash
exec "${NETWORK_AGENT_BIN}" --cubelet-config "${CUBELET_CONFIG}" --state-dir "${NETWORK_AGENT_STATE_DIR}"
```

没有传入 `--health-listen "${NETWORK_AGENT_HEALTH_ADDR:-127.0.0.1:19090}"`。

远端 `127.0.0.1:19090` 已被 `mihomo` 占用，导致 network-agent 不能按 quickcheck 预期在 `19091` 提供 `/healthz`，systemd postcheck 最终失败。

修复方向：

- 在 `deploy/one-click/scripts/systemd/network-agent-start.sh` 中补充 `--health-listen` 参数。
- 保持 `network-agent-postcheck.sh`、quickcheck 和启动参数使用同一个 `NETWORK_AGENT_HEALTH_ADDR`。

### 3. cube-proxy systemd 脚本仍依赖 x86_64 mkcert

远端检查结果：

```text
/usr/local/bin/mkcert: ELF 64-bit LSB executable, x86-64
/usr/local/services/cubetoolbox/support/bin/mkcert: ELF 64-bit LSB executable, x86-64
remote uname -m: aarch64
```

因此 `cube-proxy-start.sh` 调用 `/usr/local/bin/mkcert` 时失败：

```text
/usr/local/services/cubetoolbox/scripts/systemd/cube-proxy-start.sh: line 64: /usr/local/bin/mkcert: cannot execute binary file: Exec format error
```

当前源码中普通 one-click 路径 `deploy/one-click/scripts/one-click/up-cube-proxy.sh` 已经做过修复：

- 先检查 `mkcert -version`，确保 PATH 中或包内的 `mkcert` 可执行。
- 如果 `mkcert` 不可用，则使用 `openssl` 生成 `cube.app+3.pem` 和 `cube.app+3-key.pem`。

但 systemd 路径 `deploy/one-click/scripts/systemd/cube-proxy-start.sh` 仍是旧逻辑：

- 只判断 `command -v mkcert` 或包内文件是否存在且有可执行位。
- 不验证二进制是否能在当前架构运行。
- 没有 `openssl` fallback。

修复方向：

- 将 `up-cube-proxy.sh` 中的 `install_mkcert` 可执行性检查和 `openssl` fallback 同步到 `scripts/systemd/cube-proxy-start.sh`。
- 或者在构建 release 包时按 `ONE_CLICK_TARGET_ARCH` 打包对应架构的 `mkcert`。
- 更稳妥的方案是两者都做：优先打包正确架构 `mkcert`，并保留 `openssl` fallback 作为兜底。

临时规避方式：

```bash
mkdir -p /usr/local/services/cubetoolbox/cubeproxy/certs
openssl req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes \
  -keyout /usr/local/services/cubetoolbox/cubeproxy/certs/cube.app+3-key.pem \
  -out /usr/local/services/cubetoolbox/cubeproxy/certs/cube.app+3.pem \
  -subj "/CN=cube.app" \
  -addext "subjectAltName=DNS:cube.app,DNS:*.cube.app,DNS:localhost,IP:127.0.0.1"
```

当证书文件已存在时，`cube-proxy-start.sh` 会跳过 `mkcert`。

### 4. cubemaster 端口覆盖未完全贯通

本次 `.env` 中设置：

```bash
CUBEMASTER_ADDR=127.0.0.1:18089
```

但 cubemaster 实际配置仍显示：

```text
HttpPort: 8089
```

并且进程实际监听：

```text
*:8089
```

这会导致 postcheck/quickcheck 使用的地址和服务实际监听端口不一致。后续需要确认 systemd 路径是否应该支持 `CUBEMASTER_ADDR` 覆盖，或者限制 one-click 中该端口不可通过 `.env` 覆盖。

## 远端现场状态

本次验证结束时已停止卡住的安装任务和 systemd jobs，远端没有继续运行的 install 进程。

远端保留了本次验证产物与日志，便于后续复测：

```text
/opt/cubesandbox-oneclick-verify-a6f78b8/
```

为了满足 one-click preflight 对 `/data/cubelet` 的 XFS 要求，本次验证创建并挂载了临时 XFS loopback：

```text
/opt/cubesandbox-oneclick-verify-a6f78b8/xfs/cubelet-xfs.img -> /data/cubelet
```

该挂载当前保留，便于后续继续验证。

## 结论

当前最新提交 `a6f78b8` 的 ARM64 one-click release 构建已经成功，说明编译、guest image 生成和打包链路基本可用。

但 one-click systemd 部署链路仍未完全 ARM64-ready。主要剩余问题集中在 systemd helper 脚本和运行时默认值：

- ARM64 默认镜像选择未完全贯通。
- network-agent 启动参数与健康检查参数不一致。
- cube-proxy systemd 路径未同步 `mkcert` 可执行性检查和 `openssl` fallback。
- cubemaster 端口覆盖存在配置和 quickcheck 不一致。

下一步应优先修复 systemd one-click 脚本，使其和普通 one-click runtime 脚本保持一致，然后重新构建 release 包并在 `192.168.25.61` 上复测完整部署。
