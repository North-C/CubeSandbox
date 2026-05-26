#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd -P)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

KERNEL_TAG="${KERNEL_TAG:-6.6.119-49.6}"
KERNEL_ARCHIVE_URL="${KERNEL_ARCHIVE_URL:-https://gitee.com/OpenCloudOS/OpenCloudOS-Kernel/repository/archive/${KERNEL_TAG}.zip}"
WORK_DIR="${WORK_DIR:-${ROOT_DIR}/.work/kernel-oc9-arm64}"
SRC_DIR="${SRC_DIR:-${WORK_DIR}/linux}"
OUTPUT_DIR="${OUTPUT_DIR:-${WORK_DIR}/output}"
CONFIG_FRAGMENT="${CONFIG_FRAGMENT:-${ROOT_DIR}/configs/kernel-oc9-arm64.fragment}"
JOBS="${JOBS:-$(nproc)}"

ARCH="${ARCH:-arm64}"
CROSS_COMPILE="${CROSS_COMPILE:-}"

log() { printf '[INFO ] %s\n' "$*"; }
warn() { printf '[WARN ] %s\n' "$*" >&2; }
die() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

make_kernel() {
  local make_args=(-C "${SRC_DIR}" ARCH="${ARCH}")
  if [[ -n "${CROSS_COMPILE}" ]]; then
    make_args+=("CROSS_COMPILE=${CROSS_COMPILE}")
  fi
  make "${make_args[@]}" "$@"
}

download_and_extract_source() {
  if [[ -d "${SRC_DIR}/.git" || -f "${SRC_DIR}/Makefile" ]]; then
    log "using existing kernel source: ${SRC_DIR}"
    return 0
  fi

  require_cmd unzip
  mkdir -p "${WORK_DIR}"
  local archive="${WORK_DIR}/opencloudos-kernel-${KERNEL_TAG}.zip"

  if [[ ! -f "${archive}" ]]; then
    log "downloading kernel source: ${KERNEL_ARCHIVE_URL}"
    if command -v curl >/dev/null 2>&1; then
      curl -fL --retry 3 "${KERNEL_ARCHIVE_URL}" -o "${archive}"
    elif command -v wget >/dev/null 2>&1; then
      wget -O "${archive}" "${KERNEL_ARCHIVE_URL}"
    else
      die "neither curl nor wget is available"
    fi
  fi

  rm -rf "${WORK_DIR}/kernel-source" "${SRC_DIR}"
  mkdir -p "${WORK_DIR}/kernel-source"
  unzip -q "${archive}" -d "${WORK_DIR}/kernel-source"

  shopt -s nullglob
  local dirs=("${WORK_DIR}"/kernel-source/*/)
  shopt -u nullglob
  [[ "${#dirs[@]}" -eq 1 ]] || die "expected one extracted source directory, got ${#dirs[@]}"
  mv "${dirs[0]%/}" "${SRC_DIR}"
}

check_build_tools() {
  require_cmd make
  require_cmd bc
  require_cmd bison
  require_cmd flex
  require_cmd openssl
  require_cmd file
  if [[ -n "${CROSS_COMPILE}" ]]; then
    require_cmd "${CROSS_COMPILE}gcc"
  else
    case "$(uname -m)" in
      aarch64|arm64) ;;
      *) die "set CROSS_COMPILE=aarch64-linux-gnu- when building ARM64 Image on $(uname -m)" ;;
    esac
  fi
}

configure_kernel() {
  require_cmd python3
  [[ -f "${CONFIG_FRAGMENT}" ]] || die "config fragment not found: ${CONFIG_FRAGMENT}"

  log "configuring ${KERNEL_TAG} with arm64 defconfig plus ${CONFIG_FRAGMENT}"
  make_kernel defconfig

  local merge_script="${SRC_DIR}/scripts/kconfig/merge_config.sh"
  [[ -x "${merge_script}" ]] || die "kernel merge_config.sh not found: ${merge_script}"
  "${merge_script}" -m -O "${SRC_DIR}" "${SRC_DIR}/.config" "${CONFIG_FRAGMENT}"
  make_kernel olddefconfig
}

build_image() {
  log "building arch/arm64/boot/Image with ${JOBS} jobs"
  make_kernel -j"${JOBS}" Image

  local image_src="${SRC_DIR}/arch/arm64/boot/Image"
  [[ -s "${image_src}" ]] || die "ARM64 Image artifact not found: ${image_src}"

  mkdir -p "${OUTPUT_DIR}"
  install -m 0644 "${image_src}" "${OUTPUT_DIR}/Image"
  install -m 0644 "${SRC_DIR}/.config" "${OUTPUT_DIR}/kernel-oc9-arm64.config"

  log "artifact: ${OUTPUT_DIR}/Image"
  file "${OUTPUT_DIR}/Image"
  log "resolved config: ${OUTPUT_DIR}/kernel-oc9-arm64.config"
}

validate_config() {
  local config_path="${OUTPUT_DIR}/kernel-oc9-arm64.config"
  local key
  for key in \
    CONFIG_DEVMEM=y \
    CONFIG_CGROUPS=y \
    CONFIG_MEMCG=y \
    CONFIG_CGROUP_PIDS=y \
    CONFIG_VIRTIO_PMEM=y \
    CONFIG_BLK_DEV_PMEM=y \
    CONFIG_VIRTIO_VSOCKETS=y
  do
    grep -Fxq "${key}" "${config_path}" || die "resolved kernel config missing required option: ${key}"
  done
  log "validated required CubeSandbox ARM64 guest config options"
}

main() {
  check_build_tools
  download_and_extract_source
  configure_kernel
  build_image
  validate_config

  cat <<EOF

Next step for one-click ARM64 packaging:
  ONE_CLICK_CUBE_KERNEL_VMLINUX=${OUTPUT_DIR}/Image \\
  ONE_CLICK_CUBE_KERNEL_CONFIG=${OUTPUT_DIR}/kernel-oc9-arm64.config \\
  ONE_CLICK_TARGET_ARCH=arm64 \\
    ./deploy/one-click/build-release-bundle-builder.sh
EOF
}

main "$@"
