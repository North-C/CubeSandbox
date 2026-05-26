请把固定 kernel 制品放到本目录，默认兼容文件名如下：

- `vmlinux`
- `vmlinux-pvm`（可选，PVM guest kernel）

也可以按架构放置：

- `linux-amd64/vmlinux`
- `linux-arm64/vmlinux`
- `linux-arm64/kernel-oc9-arm64.config`

guest image 会在构建 one-click 发布包时，基于 `deploy/guest-image/Dockerfile` 在本地动态构建，不再依赖预制 zip。

如需覆盖 kernel 默认路径，可以通过环境变量指定：

- `ONE_CLICK_TARGET_ARCH`
- `ONE_CLICK_CUBE_KERNEL_VMLINUX`
- `ONE_CLICK_CUBE_KERNEL_CONFIG`
- `ONE_CLICK_CUBE_KERNEL_PVM_VMLINUX`

ARM64 普通 guest kernel 由 Linux `arch/arm64/boot/Image` 提供。当前
one-click 包会安装为 `cube-kernel-scf-linux-arm64/vmlinux` 并保留
`cube-kernel-scf` 兼容 symlink，因此 ARM64 构建时请显式传入：

```bash
ONE_CLICK_TARGET_ARCH=arm64 \
ONE_CLICK_CUBE_KERNEL_VMLINUX=/abs/path/to/Image \
ONE_CLICK_CUBE_KERNEL_CONFIG=/abs/path/to/kernel-oc9-arm64.config \
  ./deploy/one-click/build-release-bundle-builder.sh
```
