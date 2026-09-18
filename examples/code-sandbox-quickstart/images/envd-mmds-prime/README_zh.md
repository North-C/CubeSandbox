# CubeSandbox envd MMDS-prime 源码与镜像构建

本目录正式交付一个可审计、固定输入的 envd 0.5.13 构建方案：完整 envd
源码、其实际使用的本地 `shared` 依赖、上游许可证、逐文件 SHA-256、独立
MMDS-prime 补丁、amd64/arm64 编译流程、基于社区 `sandbox-code` 的双架构
镜像构建和联网/离线命令。

这不是性能结论，也不部署主机、创建 Template 或发布镜像。历史
`arm64-slim-mmds-prime-native-code-v3` 等镜像同时包含基础镜像、native code
server、启动路径或其他系统优化，不能归因于 envd；本目录不会复现或继承这些
组合改动。

## 固定输入与边界

| 输入 | 固定值 |
|---|---|
| envd 上游 | `https://github.com/e2b-dev/infra.git` |
| envd commit | `b8ca332f435370397bf42be614b2a5b620d65d39` |
| envd 版本 | `0.5.13` |
| envd 声明的 Go 版本 | `1.25.4` |
| `SOURCE_DATE_EPOCH` | `1776266298`（fixed upstream commit 时间） |
| Go builder | `golang:1.25.4-bookworm@sha256:e17419604b6d1f9bc245694425f0ec9b1b53685c80850900a376fb10cb0f70cb` |
| 社区 sandbox-code 多架构清单 | `sha256:743d264fad8c9dc9a49f07e931166d24d025363360ae770ca4b70e3f19540944` |
| amd64 基础 manifest | `sha256:467494c38f3c335e42d590e23d5cbb00dc15c2627c9da4f7e7f3bd9f4ec18c5e` |
| arm64 基础 manifest | `sha256:e1cb43e12ba70b8453b45f0c063306faab8a6974aa3fd76982dc4d019d07c60d` |
| 上游许可证 | Apache-2.0，原文见 `source/LICENSE` |

固定值的单一脚本入口是 `versions.env`；Dockerfile 同时保留安全默认值，构建
脚本会显式传入这些值。源码身份、Git tree、archive hash 与补丁沿革见
`SOURCE_PROVENANCE.md`，全部保留文件的 SHA-256 见 `SOURCE_SHA256SUMS`。

目录边界：

- `source/packages/envd/` 是 fixed commit 的完整 98 文件 envd 源码。
- `source/packages/shared/` 仅保留 envd 在 `GOWORK=off` 测试下实际依赖的六个
  shared 包及该模块的 `go.mod`/`go.sum`，用于满足上游固定的 `replace ../shared`。
- `patches/` 是唯一会应用的下游源码改动；`source/` 始终保持未打补丁状态。
- `_output/` 和离线 bundle 都是生成物，不纳入 Git。

先做不联网的来源和补丁检查：

```sh
cd examples/code-sandbox-quickstart/images/envd-mmds-prime
make verify
```

## envd 的作用、参数和端口

envd 运行在 Sandbox 应用容器内，为 E2B/CubeSandbox SDK 提供命令、PTY、进程、
文件、环境变量、端口转发和初始化接口。它不是 CubeSandbox 的 guest PID 1，也
不是负责创建容器的 `cube-agent`。

本固定版本的主要启动参数：

| 参数 | 含义 |
|---|---|
| `-port` | HTTP/Connect RPC 监听端口，默认 `49983` |
| `-isnotfc` | 非 Firecracker 本地调试模式；同时改变 MMDS/日志行为，生产镜像不使用 |
| `-cmd` | envd 启动时执行一次命令；上游标记为旧模板路径，本镜像不使用 |
| `-cgroup-root` | cgroup 根目录，默认 `/sys/fs/cgroup` |
| `-version` | 打印 envd 版本 |
| `-commit` | 打印编译时注入的上游 commit |
| `-prime-mmds-until-unix` | 本补丁增加的绝对 Unix 截止时间；大于 0 时启用 prime 模式 |

端口与最小健康检查：

- `49983`：envd 数据面；`GET /health` 成功时返回 HTTP `204`，没有响应体。
- `49999`：基础镜像内 lightweight code interpreter 的业务端口；`GET /health`
  用于业务 readiness，`RunCode`/`POST /execute` 走此端口。
- `8888`：启动脚本内部使用的 Jupyter Server，默认仅监听 `127.0.0.1`，不需要
  作为 CubeSandbox 暴露端口。

## MMDS-prime、snapshot 和 PostInit

上游 `PollForMMDSOpts` 每 50 ms 请求一次 Firecracker MMDS；获取到合法数据后会
立即写入环境/运行时文件并返回。如果 MMDS 尚不可用，原逻辑会继续重试。

补丁增加以下可核验行为：

1. 启动脚本默认计算 `当前 Unix 时间 + 10 秒`，通过
   `-prime-mmds-until-unix` 传给 envd；可用非负整数环境变量
   `MMDS_PRIME_SECONDS` 调整，设为 `0` 则不传 prime 参数。
2. prime 模式用独立 context 启动原 MMDS 轮询，并每 50 ms 检查绝对截止时间。
   轮询若先成功，本来就会返回；否则到期时 cancel。
3. Template snapshot capture 会保存进程内的绝对截止值、guest wall clock 与 goroutine
   状态。只有在 capture 时截止时间仍未到且 MMDS 仍未成功，才会保存“正在 prime”
   的状态；因此需按现场 capture 时长选择窗口并实测，不能默认 10 秒一定覆盖。
4. snapshot restore 后，CubeShim 在 guest agent 连接成功后调用 `reset_guest`，取
   host `Utc::now()` 发起 `SetGuestDateTime`；guest agent 最终调用
   `settimeofday`。当恢复发生在绝对截止时间之后，envd 下一次 50 ms 检查会取消
   轮询；若恢复仍在窗口内，则持续到截止时间或先成功。
5. prime 模式禁止 `/init` (`PostInit`) 再启动上游那条最长 60 秒的 MMDS 轮询。
   它只改变这一刷新路径，不把 API 标记成 `isNotFC`，所以恢复时的 MMDS
   access-token hash 校验仍保持 Firecracker 语义。

第 5 点是本次重新审计后的必要修正。历史源码补丁
`a8ee9c7d3039dc057be1381efb972df5da9f870e` 与 focused 提交
`81de92d8602d4438443cf79225c2a910dbb273cc` 把两种状态合并，可能同时绕过上述
token 校验。新补丁增加回归测试，详细来源和 hash 见 `SOURCE_PROVENANCE.md`。

## 构建依赖和镜像分层

必需工具：

- Docker Engine 和 Docker buildx/BuildKit；脚本使用 named build context、
  `--metadata-file` 以及 local、Docker archive、cache-only exporter。镜像脚本以
  固定 `SOURCE_DATE_EPOCH` 配合 `rewrite-timestamp` 导出后再加载，避免把本机构建
  时间写入 manifest。
- 本次验证版本为 Docker Engine `29.5.2`、buildx `0.34.0`、BuildKit
  `v0.30.0`。其他版本只有在支持上述参数和 `COPY --chmod` 时才可使用，升级或
  降级后应重跑本目录全部门禁。
- 联网构建需要访问 Docker Hub、CubeSandbox 镜像仓库和 Go module source/proxy。
- Go 依赖版本由 `go.mod`/`go.sum` 固定；实际编译在固定 digest 的 Go 1.25.4
  builder 中执行，不读取宿主机 Go module cache。
- `git` 在 builder 中用于 `git apply --check`；补丁不依赖本机 checkout。
- 主机侧还需 POSIX shell、`git`、GNU `sha256sum`/`tar`、`gzip`、`file`、`curl`
  和可读取 build info 的 `go` 命令；`shellcheck` 是推荐的附加静态检查工具。
- 只做交叉编译和镜像组装时无需 QEMU：builder 固定在 `$BUILDPLATFORM`，Go 负责
  交叉编译，最终基础阶段没有 `RUN`。在 amd64 主机上运行 arm64 镜像 smoke test
  才需要已注册的 binfmt/QEMU，反之亦然。

CubeSandbox 组件边界：

- fixed `sandbox-code` 基础镜像提供 Debian/Python、Jupyter、
  `/opt/lightweight-code-interpreter/server:app` 业务 code server 和原始容器配置。
- 本 Dockerfile 只替换 `/usr/bin/envd` 和
  `/usr/local/bin/start-lightweight-code-interpreter.sh`；不编译或替换业务 server。
- `cube-init` 是 guest image 中的 `/sbin/init`，`cube-agent.ext4` 是独立 guest
  agent plane；它们由仓库根目录 `make pmem-assets` / one-click guest-image 流程
  生成，不属于 OCI `sandbox-code` 应用镜像，也不由本目录修改。
- 创建可运行 Template 仍需要与目标 CubeSandbox 版本匹配的 guest kernel、guest
  image、`cube-init`、`cube-agent.ext4`、CubeShim/Cubelet 和基础镜像 registry
  可达性。本目录的镜像构建成功不能代替这些部署条件。

## 联网构建

默认门禁使用 `GOWORK=off` 和固定 builder，执行全包 test compile、认证/prime
回归以及不依赖特权环境的单元测试：

```sh
make test
```

完整上游测试不跳过 cgroup、mount、FUSE 用例：

```sh
make test-full
```

普通 BuildKit 环境通常会因只读 `/sys/fs/cgroup`、缺少 mount 权限或 `bindfs`
失败；这应记录为测试环境缺口，不能改成伪成功。要让它通过需准备隔离的 privileged
测试环境和 `bindfs`，不得直接把宿主 cgroup 暴露给不受控测试。

编译两个静态 envd 二进制：

```sh
make build-envd
file _output/envd/amd64/envd _output/envd/arm64/envd
go version -m _output/envd/amd64/envd
go version -m _output/envd/arm64/envd
sha256sum _output/envd/amd64/envd _output/envd/arm64/envd
```

构建并加载两个单架构镜像：

```sh
make images
```

默认 tag：

- `cubesandbox-dev/sandbox-code:envd-mmds-prime-0.5.13-amd64`
- `cubesandbox-dev/sandbox-code:envd-mmds-prime-0.5.13-arm64`

也可显式指定 tag：

```sh
./scripts/build-image.sh amd64 example.invalid/team/sandbox-code:envd-prime-amd64
./scripts/build-image.sh arm64 example.invalid/team/sandbox-code:envd-prime-arm64
```

脚本把 BuildKit metadata 写入 `_output/images/*-metadata.json`，其中同时记录
本地 manifest digest 和 config digest。若后续发布到授权 registry，应另外回读
registry manifest digest；本地 digest 不能冒充尚未发布的 registry digest，Template
必须引用发布后回读的 digest 而不是可变 tag。

二进制和镜像构建均显式使用 `--no-cache`，并以 fixed upstream commit 时间作为
`SOURCE_DATE_EPOCH`，避免未记录的本机 BuildKit layer 污染产物。

## 离线构建

在联网、与离线构建机同 CPU 架构的准备机上生成显式 bundle。bundle 包含固定
builder、本次两个基础 manifest、`go mod vendor` 结果、元数据和 SHA256SUMS：

```sh
./scripts/prepare-offline-bundle.sh ./offline-bundle
tar -C . -czf envd-mmds-prime-offline-bundle.tar.gz offline-bundle
```

把仓库和 bundle 传到离线机，解包后执行：

```sh
./scripts/build-offline.sh ./offline-bundle amd64
./scripts/build-offline.sh ./offline-bundle arm64
```

离线脚本先执行 `sha256sum -c`，核对 host 架构、envd commit 和补丁 SHA，再
`docker load` 固定镜像，以 `--pull=false --no-cache --network none`、
`GOWORK=off`、`-mod=vendor` 构建。联网路径在下载后也转为同一 vendor 模式，
使两条路径的 envd 二进制哈希一致。bundle 不是 Git 产物；分发前还应按组织要求
审计其中所有第三方依赖许可证。

本次实测记录、产物哈希、镜像 digest 和环境缺口见 `VALIDATION.md`。

## 产物、架构和最小 smoke test

不运行目标架构也能检查 OCI 架构、envd ELF、Go build info 和启动脚本哈希：

```sh
./scripts/inspect-image.sh cubesandbox-dev/sandbox-code:envd-mmds-prime-0.5.13-amd64
./scripts/inspect-image.sh cubesandbox-dev/sandbox-code:envd-mmds-prime-0.5.13-arm64
```

运行态最小 smoke test 同时检查 envd 49983 和业务 49999：

```sh
./scripts/smoke-image.sh cubesandbox-dev/sandbox-code:envd-mmds-prime-0.5.13-amd64 amd64
./scripts/smoke-image.sh cubesandbox-dev/sandbox-code:envd-mmds-prime-0.5.13-arm64 arm64
```

脚本使用随机 loopback host port、等待最多 120 秒并精确清理自己创建的容器。
跨架构运行失败时先检查 `docker buildx inspect --bootstrap` 和
`/proc/sys/fs/binfmt_misc/`；不要把“能交叉编译”误报成“arm64 运行 smoke 通过”。

## Template 使用注意事项

本目录不推送镜像、不创建或修改 Template。获得授权并把镜像发布到目标节点可访问
的 registry 后，使用不可变 digest，例如：

```sh
cubemastercli tpl create-from-image \
  --image '<registry>/<repo>@sha256:<manifest-digest>' \
  --writable-layer-size 1G \
  --expose-port 49983 \
  --expose-port 49999 \
  --probe 49999
```

- code-interpreter Template 的 readiness 建议探测业务端口 `49999`；它证明业务
  gateway 响应，但不能单独证明 envd `49983`、Jupyter `8888` 或 MMDS prime 状态。
  验证时应分别检查两个公开健康端点和 envd cmdline。
- 只使用 envd、没有业务 server 的自定义镜像可改为 `--probe 49983`，但不能照抄
  本基础镜像的启动命令。
- snapshot capture 前记录 envd PID/cmdline、绝对截止值、49983/49999/8888 状态；
  restore 后确认 guest 时间已重置、deadline 已过且不再发生持续 MMDS 重试。
- 对比性能时必须使用同一 host、资源、基础镜像 digest、业务 server、Template
  capture 状态和交替轮次。这里交付的是构建输入，不是 envd 单变量性能证明。

## 历史证据与明确排除项

- `b7d9402c9cf63e796405484aab1e2c624a3ec5a1` 同时构建了 envd 和 native code
  server，且继承历史 benchmark base；它是构建方式参考，不是本方案的单变量产物。
- `envd-mmds-prime-community-aligned-20260808` 证明曾经按平台替换 envd/启动脚本并
  完成健康检查，但其 amd64/arm64 分别使用不同 envd revision；本方案重新统一到
  fixed envd 0.5.13，不能冒充原归档的二进制或 digest。
- `arm64-slim-mmds-prime-native-code-v3`、slim/native-code 等组合镜像明确排除，
  不用于说明 envd 效果。
