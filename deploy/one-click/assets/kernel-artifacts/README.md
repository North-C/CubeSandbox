请把固定 kernel 制品按目标架构放到本目录，默认路径如下：

- `linux-amd64/vmlinux`
- `linux-arm64/vmlinux`

guest image 会在构建 one-click 发布包时，基于 `deploy/guest-image/Dockerfile` 在本地动态构建，不再依赖预制 zip。

如需覆盖 kernel 默认路径，可以通过环境变量指定：

- `ONE_CLICK_TARGET_ARCH`
- `ONE_CLICK_CUBE_KERNEL_VMLINUX`

兼容说明：未显式设置 `ONE_CLICK_CUBE_KERNEL_VMLINUX` 时，amd64 构建仍会接受历史路径 `assets/kernel-artifacts/vmlinux`。
