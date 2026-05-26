# Guest Kernel Builds

This directory contains helper scripts for guest kernels that are not PVM
specific.

## ARM64 OpenCloudOS 9 6.6.x Guest Image

`build-oc9-arm64-guest-image.sh` builds an ARM64 guest kernel `Image` from the
OpenCloudOS `6.6.119-49.6` kernel source by default. It starts from the kernel
tree's `arm64 defconfig`, applies `configs/kernel-oc9-arm64.fragment`, then runs
`olddefconfig`.

The fragment is aligned with `configs/kernel-oc9.config` for the guest-facing
features CubeSandbox needs: cgroups, namespaces, vsock, virtio, pmem/DAX,
overlayfs, ext4, netfilter, and `/dev/mem`.

Native ARM64 build:

```bash
./deploy/kernel/build-oc9-arm64-guest-image.sh
```

Cross build from x86_64:

```bash
CROSS_COMPILE=aarch64-linux-gnu- ./deploy/kernel/build-oc9-arm64-guest-image.sh
```

Default output:

```text
.work/kernel-oc9-arm64/output/Image
.work/kernel-oc9-arm64/output/kernel-oc9-arm64.config
```

Use the resulting `Image` as the ordinary guest kernel input for one-click
packaging:

```bash
ONE_CLICK_TARGET_ARCH=arm64 \
ONE_CLICK_CUBE_KERNEL_VMLINUX=.work/kernel-oc9-arm64/output/Image \
ONE_CLICK_CUBE_KERNEL_CONFIG=.work/kernel-oc9-arm64/output/kernel-oc9-arm64.config \
  ./deploy/one-click/build-release-bundle-builder.sh
```

The release package installs the assets as
`cube-kernel-scf-linux-arm64/vmlinux` and
`cube-image-linux-arm64/cube-guest-image-cpu.img`, with compatibility symlinks
named `cube-kernel-scf` and `cube-image`. The ARM64 loader path consumes the
Linux ARM64 `Image` format even though the installed runtime filename remains
`vmlinux`.
