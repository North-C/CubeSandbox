#!/usr/bin/env bash
set -euo pipefail

ONE_CLICK_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ONE_CLICK_DIR="$(cd "${ONE_CLICK_LIB_DIR}/.." && pwd)"

log() {
  echo "[one-click] $*" >&2
}

die() {
  echo "[one-click] ERROR: $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "${cmd}" >/dev/null 2>&1 || die "required command not found: ${cmd}"
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    die "this script must run as root"
  fi
}

load_env_file() {
  local env_file="$1"
  local had_nounset=0
  [[ -n "${env_file}" ]] || return 0
  [[ -f "${env_file}" ]] || die "env file not found: ${env_file}"
  log "loading env file: ${env_file}"
  [[ $- == *u* ]] && had_nounset=1
  set +u
  set -a
  # shellcheck disable=SC1090
  source "${env_file}"
  set +a
  if [[ "${had_nounset}" == "1" ]]; then
    set -u
  fi
}

ensure_file() {
  local path="$1"
  [[ -f "${path}" ]] || die "required file not found: ${path}"
}

ensure_dir() {
  local path="$1"
  [[ -d "${path}" ]] || die "required directory not found: ${path}"
}

copy_file() {
  local src="$1"
  local dst="$2"
  ensure_file "${src}"
  mkdir -p "$(dirname "${dst}")"
  cp -f "${src}" "${dst}"
}

copy_dir_contents() {
  local src="$1"
  local dst="$2"
  ensure_dir "${src}"
  rm -rf "${dst}"
  mkdir -p "${dst}"
  cp -a "${src}/." "${dst}/"
}

stable_dir_hash() {
  local dir="$1"
  local path
  ensure_dir "${dir}"

  (
    cd "${dir}"
    while IFS= read -r -d '' path; do
      printf '%s\n' "${path}"
      sha256sum "${path}"
    done < <(find . -type f -print0 | sort -z)
  ) | sha256sum | awk '{print $1}'
}

latest_git_revision() {
  local repo_root="$1"
  if command -v git >/dev/null 2>&1 && git -C "${repo_root}" rev-parse --short HEAD >/dev/null 2>&1; then
    git -C "${repo_root}" rev-parse --short HEAD
    return 0
  fi
  date +%Y%m%d-%H%M%S
}

normalize_one_click_arch() {
  local raw="${1:-}"
  case "${raw}" in
    amd64|x86_64|linux/amd64|linux-amd64)
      printf 'amd64\n'
      ;;
    arm64|aarch64|linux/arm64|linux-arm64)
      printf 'arm64\n'
      ;;
    *)
      die "unsupported one-click target arch: ${raw:-<empty>} (expected amd64 or arm64)"
      ;;
  esac
}

detect_host_one_click_arch() {
  normalize_one_click_arch "$(uname -m)"
}

one_click_target_arch() {
  if [[ -n "${ONE_CLICK_TARGET_ARCH:-}" ]]; then
    normalize_one_click_arch "${ONE_CLICK_TARGET_ARCH}"
    return 0
  fi

  detect_host_one_click_arch
}

one_click_linux_platform() {
  local arch="$1"
  printf 'linux/%s\n' "$(normalize_one_click_arch "${arch}")"
}

one_click_rust_musl_triple() {
  local arch
  arch="$(normalize_one_click_arch "$1")"
  case "${arch}" in
    amd64) printf 'x86_64-unknown-linux-musl\n' ;;
    arm64) printf 'aarch64-unknown-linux-musl\n' ;;
  esac
}

one_click_default_guest_image_base_ref() {
  local arch
  arch="$(normalize_one_click_arch "$1")"
  case "${arch}" in
    amd64) printf 'cube-sandbox-image.tencentcloudcr.com/opensource/tencentos4-minimal:4.4-v20250331\n' ;;
    arm64) printf 'openeuler/openeuler:24.03-lts-sp3\n' ;;
  esac
}

one_click_default_kernel_vmlinux() {
  local artifact_dir="$1"
  local arch
  arch="$(normalize_one_click_arch "$2")"

  if [[ -f "${artifact_dir}/linux-${arch}/vmlinux" ]]; then
    printf '%s\n' "${artifact_dir}/linux-${arch}/vmlinux"
    return 0
  fi

  printf '%s\n' "${artifact_dir}/vmlinux"
}

one_click_default_pvm_vmlinux() {
  local artifact_dir="$1"
  local arch
  arch="$(normalize_one_click_arch "$2")"

  if [[ -f "${artifact_dir}/linux-${arch}/vmlinux-pvm" ]]; then
    printf '%s\n' "${artifact_dir}/linux-${arch}/vmlinux-pvm"
    return 0
  fi

  printf '%s\n' "${artifact_dir}/vmlinux-pvm"
}

one_click_default_kernel_config() {
  local artifact_dir="$1"
  local arch
  local kernel_path="$3"
  local kernel_dir
  arch="$(normalize_one_click_arch "$2")"
  kernel_dir="$(dirname "${kernel_path}")"

  local candidate
  local candidates=(
    "${kernel_dir}/kernel-oc9-${arch}.config"
    "${kernel_dir}/kernel-oc9.config"
    "${artifact_dir}/linux-${arch}/kernel-oc9-${arch}.config"
    "${artifact_dir}/linux-${arch}/kernel-oc9.config"
    "${artifact_dir}/kernel-oc9-${arch}.config"
    "${artifact_dir}/kernel-oc9.config"
  )
  if [[ "${arch}" == "arm64" ]]; then
    candidates=(
      "${kernel_dir}/kernel-oc9-arm64.config"
      "${candidates[@]}"
      "${artifact_dir}/linux-arm64/kernel-oc9-arm64.config"
      "${artifact_dir}/kernel-oc9-arm64.config"
    )
  fi

  for candidate in "${candidates[@]}"; do
    if [[ -f "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done
}

validate_one_click_file_arch() {
  local path="$1"
  local expected_arch="$2"
  local description="$3"
  local file_output

  require_cmd file
  ensure_file "${path}"
  expected_arch="$(normalize_one_click_arch "${expected_arch}")"
  file_output="$(file -L "${path}")"
  case "${expected_arch}" in
    amd64)
      [[ "${file_output}" == *"x86-64"* || "${file_output}" == *"x86_64"* ]] || \
        die "${description} is not amd64: ${file_output}"
      ;;
    arm64)
      [[ "${file_output}" == *"ARM aarch64"* || "${file_output}" == *"aarch64"* || "${file_output}" == *"ARM64"* ]] || \
        die "${description} is not arm64: ${file_output}"
      ;;
  esac
  log "${description}: ${file_output}"
}

ensure_native_one_click_build_arch() {
  local expected_arch="$1"
  local component="$2"
  local host_arch

  expected_arch="$(normalize_one_click_arch "${expected_arch}")"
  host_arch="$(detect_host_one_click_arch)"
  if [[ "${host_arch}" != "${expected_arch}" ]]; then
    die "${component} local build requires a native ${expected_arch} builder; current host is ${host_arch}. Use prebuilt ONE_CLICK_*_BIN overrides or build on a native ${expected_arch} host."
  fi
}

container_exists() {
  local name="$1"
  docker ps -a --format '{{.Names}}' | rg -x "${name}" >/dev/null 2>&1
}

wait_for_http() {
  local url="$1"
  local retries="${2:-30}"
  local delay="${3:-2}"
  local i
  for ((i = 1; i <= retries; i++)); do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep "${delay}"
  done
  return 1
}

wait_for_pidfile() {
  local pid_file="$1"
  local retries="${2:-20}"
  local delay="${3:-1}"
  local i
  for ((i = 1; i <= retries; i++)); do
    if [[ -f "${pid_file}" ]]; then
      local pid
      pid="$(<"${pid_file}")"
      if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
        return 0
      fi
    fi
    sleep "${delay}"
  done
  return 1
}

one_click_deploy_role() {
  local role="${ONE_CLICK_DEPLOY_ROLE:-control}"
  case "${role}" in
    control|compute)
      printf '%s\n' "${role}"
      ;;
    *)
      die "unsupported ONE_CLICK_DEPLOY_ROLE: ${role}"
      ;;
  esac
}

is_compute_role() {
  [[ "$(one_click_deploy_role)" == "compute" ]]
}

upsert_env_kv() {
  local env_file="$1"
  local key="$2"
  local value="$3"
  local tmp_file
  tmp_file="$(mktemp)"
  local replaced=false

  if [[ -f "${env_file}" ]]; then
    while IFS= read -r line || [[ -n "${line}" ]]; do
      if [[ "${line}" == "${key}="* ]]; then
        printf '%s=%s\n' "${key}" "${value}" >> "${tmp_file}"
        replaced=true
      else
        printf '%s\n' "${line}" >> "${tmp_file}"
      fi
    done < "${env_file}"
  fi

  if [[ "${replaced}" != "true" ]]; then
    printf '%s=%s\n' "${key}" "${value}" >> "${tmp_file}"
  fi

  mv -f "${tmp_file}" "${env_file}"
}

detect_pkg_manager() {
  if command -v apt-get >/dev/null 2>&1; then
    printf 'apt'
  elif command -v yum >/dev/null 2>&1; then
    printf 'yum'
  else
    die "unsupported package manager: neither apt-get nor yum found"
  fi
}

install_ripgrep() {
  if command -v rg >/dev/null 2>&1; then
    return 0
  fi
  local pm
  pm="$(detect_pkg_manager)"
  log "installing ripgrep via ${pm}..."
  case "${pm}" in
    apt)
      apt-get update -qq && apt-get install -y -qq ripgrep
      ;;
    yum)
      yum install -y epel-release 2>/dev/null || true
      yum install -y ripgrep
      ;;
  esac
  command -v rg >/dev/null 2>&1 || die "failed to install ripgrep"
}

install_docker() {
  if command -v docker >/dev/null 2>&1; then
    return 0
  fi
  local pm
  pm="$(detect_pkg_manager)"
  log "installing docker via ${pm}..."
  case "${pm}" in
    apt)
      apt-get update -qq
      apt-get install -y -qq docker.io docker-compose
      ;;
    yum)
      yum install -y docker docker-compose
      ;;
  esac
  systemctl enable docker && systemctl start docker
  command -v docker >/dev/null 2>&1 || die "failed to install docker"
}

install_docker_compose() {
  if docker compose version >/dev/null 2>&1; then
    return 0
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    return 0
  fi
  local pm
  pm="$(detect_pkg_manager)"
  log "installing docker-compose via ${pm}..."
  case "${pm}" in
    apt)
      apt-get update -qq && apt-get install -y -qq docker-compose
      ;;
    yum)
      yum install -y docker-compose
      ;;
  esac
  if ! docker compose version >/dev/null 2>&1 && ! command -v docker-compose >/dev/null 2>&1; then
    die "failed to install docker-compose"
  fi
}

install_dependencies() {
  log "checking and installing dependencies..."
  install_ripgrep
  install_docker
  install_docker_compose
}

detect_node_ip() {
  if [[ -n "${CUBE_SANDBOX_NODE_IP:-}" ]]; then
    printf '%s\n' "${CUBE_SANDBOX_NODE_IP}"
    return 0
  fi

  local detected_ip=""
  if command -v ip >/dev/null 2>&1; then
    local detected_iface
    detected_iface="$(detect_primary_interface || true)"
    if [[ -n "${detected_iface}" ]]; then
      detected_ip="$(ip -4 addr show dev "${detected_iface}" 2>/dev/null \
        | grep -oP 'inet \K[0-9.]+' | head -1 || true)"
      if [[ -n "${detected_ip}" ]]; then
        log "auto-detected node IP from ${detected_iface}: ${detected_ip}"
        printf '%s\n' "${detected_ip}"
        return 0
      fi
    fi

    detected_ip="$(ip -4 addr show scope global 2>/dev/null \
      | grep -oP 'inet \K[0-9.]+' | head -1 || true)"
  fi

  if [[ -n "${detected_ip}" ]]; then
    log "auto-detected node IP from first global IPv4 address: ${detected_ip}"
    printf '%s\n' "${detected_ip}"
    return 0
  fi

  die "cannot auto-detect node IP. Please set CUBE_SANDBOX_NODE_IP or pass --node-ip=<ip>"
}

detect_primary_interface() {
  # Honor explicit override first.
  if [[ -n "${CUBE_SANDBOX_ETH_NAME:-}" ]]; then
    printf '%s\n' "${CUBE_SANDBOX_ETH_NAME}"
    return 0
  fi

  # `ip` is required for auto-detection.
  command -v ip >/dev/null 2>&1 || return 1

  local iface
  # Preferred path: resolve interface from default IPv4 route.
  iface="$(ip -o -4 route show to default 2>/dev/null | awk '{print $5; exit}')"
  if [[ -n "${iface}" ]]; then
    printf '%s\n' "${iface}"
    return 0
  fi

  # Fallback: first non-loopback interface that is currently up.
  iface="$(ip -o link show up 2>/dev/null \
    | awk -F': ' '$2 != "lo" {print $2; exit}' \
    | cut -d@ -f1)"
  [[ -n "${iface}" ]] || return 1
  printf '%s\n' "${iface}"
}

ensure_kernel_vmlinux() {
  local vmlinux_path="$1"
  local default_dir="$2"

  if [[ -f "${vmlinux_path}" ]]; then
    return 0
  fi

  cat >&2 <<EOF

============================================================
  ERROR: Kernel vmlinux file not found!
============================================================

  Missing: ${vmlinux_path}

  The vmlinux file is a required Linux kernel image used to
  boot guest VMs. You must provide it before building.

  How to fix:

    Option A — Place it in the default location:

      cp /path/to/your/vmlinux ${default_dir}/vmlinux

    Option B — Set a custom path via environment variable:

      export ONE_CLICK_CUBE_KERNEL_VMLINUX=/path/to/vmlinux

  Then re-run the build script.

  For more details, see: docs/guide/one-click-deploy.md
============================================================

EOF
  exit 1
}
