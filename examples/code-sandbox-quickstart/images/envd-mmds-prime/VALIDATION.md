# 2026-09-18 验证记录

本记录只覆盖当前目录的固定源码、补丁、二进制和本地镜像。没有推送镜像、部署
远端服务、创建或修改 Template。以下 digest 是本地 BuildKit 产物；发布到 registry
后必须重新回读 registry digest。

## 环境

- 构建主机：`linux/amd64`（`x86_64`）
- Docker client/server：`29.5.2`
- buildx：`0.34.0`
- BuildKit：`v0.30.0`
- 宿主 Go：`1.26.2`，仅用于读取 build info；编译固定使用容器内 Go `1.25.4`
- ARM64 smoke 主机：用户指定的 `.61`，原生 `aarch64`，Docker `29.4.3`

## 已通过

- `make verify`：155 个保留源文件 SHA-256 全部通过；审计补丁可对未修改的固定
  源码执行 `git apply --check`。
- `make test`：`GOWORK=off` 下全包 test compile、
  `TestValidateInitAccessToken`、`TestCheckMMDSHash`（含 prime 模式认证回归）以及
  `internal/host`、`internal/services/legacy`、`internal/utils` 通过。
- `make build-envd`：双架构静态 ELF、Go build info 与 `GOARCH` 硬校验通过。
- `make images`：两个 digest-pinned 基础镜像均构建并 `--load` 成功。
- 联网/离线一致性：离线 bundle 校验通过；amd64/arm64 都以
  `--pull=false --no-cache --network none -mod=vendor` 构建成功，日志无 registry
  auth/pull。固定 `SOURCE_DATE_EPOCH` 并使用 BuildKit `rewrite-timestamp` 后，两条
  路径的 envd SHA-256 和本地 manifest digest 一致；amd64 离线无缓存构建连续
  两次也得到同一 digest。
- amd64 smoke：本机构建主机上的 envd `49983/health`（HTTP 204）和业务
  `49999/health`（HTTP 200）通过。
- arm64 smoke：最终 ARM64 镜像在 `.61` 原生 AArch64 主机上的上述两个健康检查
  通过；唯一命名的临时容器和测试镜像标签已删除。

## 产物身份

| 架构 | envd SHA-256 | 本地 manifest digest | config digest |
|---|---|---|---|
| amd64 | `a9f2136a5433170c9fdf9f5070ffd6969e7fb38da597edcd8baed2ef8400ed8f` | `sha256:7cd15eedb5cf676259f9dfe888d88089f4ea942c640a6c4ae6248e43232782af` | `sha256:70c0106499aad55bdeed0598a3c7bfd547236a91058e7467824a39de28062fdb` |
| arm64 | `a05ce573efe15d170ad880e02a2bfb14e39b57bbd301bda959c99bcc03f6adfb` | `sha256:ae12864017fb4e0bc0011d5bd2b4c485186ede11d68c6a3af42a06a7c7a95e9a` | `sha256:ace43b2bf7f895c58a8ffbd4842994c5cdacf8dd2ef49d4e64a272b59c7bd2ba` |

镜像内启动脚本两架构 SHA-256 均为
`194aca23a35f36f072d0de8610e4f0af37b340d904ebab93a86bc0c908ffaa28`。

## 明确未通过或未执行

- `make test-full` 未通过，不能标记为成功。上游完整测试要求当前普通 BuildKit
  容器不具备的环境：可写 `/sys/fs/cgroup`、mount/FUSE 权限和 `bindfs`。实际失败
  包括 `TestProcessFile` 的只读 cgroup/mount exit 32、`TestCgroupRoundTrip` 的只读
  cgroup、`TestIsPathOnNetworkMount_FuseMount` 缺少 `bindfs`。非特权默认门禁及
  相关目标测试均已通过。
- 本机未注册 ARM64 binfmt/QEMU，直接运行 ARM64 镜像得到 `exec format error`；
  因此 ARM64 运行态验证改在原生 `.61` 完成，不能把本机交叉构建当作运行成功。
- 当前主机未安装 `shellcheck`；所有 shell 脚本已通过 `sh -n`，但没有伪造
  shellcheck 结果。
- 没有发布 registry manifest、构建多架构 index、推送镜像或操作 Template。
