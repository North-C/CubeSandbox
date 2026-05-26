#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENV_FILE="${ONE_CLICK_ENV_FILE:-${SCRIPT_DIR}/.env}"
if [[ -f "${ENV_FILE}" ]]; then
  load_env_file "${ENV_FILE}"
fi

PREBUILT_DIR="${SCRIPT_DIR}/.work/prebuilt"
HELPER_SCRIPT="${SCRIPT_DIR}/.work/build-prebuilt-in-builder.sh"
BUILDER_IMAGE_REF="${BUILDER_IMAGE:-cube-sandbox-builder:latest}"
TARGET_ARCH="$(one_click_target_arch)"
TARGET_PLATFORM="$(one_click_linux_platform "${TARGET_ARCH}")"
RUST_MUSL_TRIPLE="$(one_click_rust_musl_triple "${TARGET_ARCH}")"

require_cmd docker
require_cmd make

rm -rf "${PREBUILT_DIR}"
mkdir -p "${PREBUILT_DIR}" "$(dirname "${HELPER_SCRIPT}")"

cat > "${HELPER_SCRIPT}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

PREBUILT_DIR="/workspace/deploy/one-click/.work/prebuilt"
TARGET_ARCH="${ONE_CLICK_TARGET_ARCH:?}"
TARGET_PLATFORM="${ONE_CLICK_TARGET_PLATFORM:?}"
RUST_MUSL_TRIPLE="${ONE_CLICK_RUST_MUSL_TRIPLE:?}"
mkdir -p "${PREBUILT_DIR}"
rm -f \
  "${PREBUILT_DIR}/cubemaster" \
  "${PREBUILT_DIR}/cubemastercli" \
  "${PREBUILT_DIR}/cubelet" \
  "${PREBUILT_DIR}/cubecli" \
  "${PREBUILT_DIR}/cube-api" \
  "${PREBUILT_DIR}/network-agent" \
  "${PREBUILT_DIR}/cube-agent" \
  "${PREBUILT_DIR}/containerd-shim-cube-rs" \
  "${PREBUILT_DIR}/cube-runtime"

echo "[one-click] building cubemaster in builder" >&2
(cd /workspace/CubeMaster && go mod download && GOOS=linux GOARCH="${TARGET_ARCH}" go build -o "${PREBUILT_DIR}/cubemaster" ./cmd/cubemaster)

echo "[one-click] building cubemastercli in builder" >&2
(cd /workspace/CubeMaster && GOOS=linux GOARCH="${TARGET_ARCH}" go build -o "${PREBUILT_DIR}/cubemastercli" ./cmd/cubemastercli)

echo "[one-click] building cubelet in builder" >&2
(cd /workspace/Cubelet && go mod download && GOOS=linux GOARCH="${TARGET_ARCH}" go build -o "${PREBUILT_DIR}/cubelet" ./cmd/cubelet)

echo "[one-click] building cubecli in builder" >&2
(cd /workspace/Cubelet && GOOS=linux GOARCH="${TARGET_ARCH}" go build -o "${PREBUILT_DIR}/cubecli" ./cmd/cubecli)

echo "[one-click] building cube-api in builder" >&2
case "$(uname -m)" in
  x86_64) BUILDER_NATIVE_ARCH=amd64 ;;
  aarch64|arm64) BUILDER_NATIVE_ARCH=arm64 ;;
  *) echo "[one-click] unsupported builder host architecture: $(uname -m)" >&2; exit 1 ;;
esac
if [[ "${BUILDER_NATIVE_ARCH}" != "${TARGET_ARCH}" ]]; then
  echo "[one-click] cube-api requires a native ${TARGET_ARCH} builder; builder is ${BUILDER_NATIVE_ARCH}" >&2
  echo "[one-click] For cross-arch packaging with prebuilt Rust artifacts, call build-release-bundle.sh directly with ONE_CLICK_*_BIN overrides." >&2
  exit 1
fi
(cd /workspace/CubeAPI && cargo build --release --locked)
install -m 0755 /workspace/CubeAPI/target/release/cube-api "${PREBUILT_DIR}/cube-api"

echo "[one-click] building network-agent in builder" >&2
(cd /workspace/network-agent && GOOS=linux GOARCH="${TARGET_ARCH}" go build -o "${PREBUILT_DIR}/network-agent" ./cmd/network-agent)

echo "[one-click] building cube-agent in builder" >&2
(cd /workspace/agent && HOST_ARCH="${TARGET_ARCH}" TRIPLE="${RUST_MUSL_TRIPLE}" make -j1)
install -m 0755 "/workspace/agent/target/${RUST_MUSL_TRIPLE}/release/cube-agent" "${PREBUILT_DIR}/cube-agent"

echo "[one-click] building shim workspace in builder" >&2
if [[ "${BUILDER_NATIVE_ARCH}" != "${TARGET_ARCH}" ]]; then
  echo "[one-click] CubeShim requires a native ${TARGET_ARCH} builder; builder is ${BUILDER_NATIVE_ARCH}" >&2
  echo "[one-click] For cross-arch packaging with prebuilt Rust artifacts, call build-release-bundle.sh directly with ONE_CLICK_*_BIN overrides." >&2
  exit 1
fi
(cd /workspace/CubeShim && cargo build --release --locked)
install -m 0755 /workspace/CubeShim/target/release/containerd-shim-cube-rs "${PREBUILT_DIR}/containerd-shim-cube-rs"
install -m 0755 /workspace/CubeShim/target/release/cube-runtime "${PREBUILT_DIR}/cube-runtime"
EOF

chmod 0755 "${HELPER_SCRIPT}"

if ! docker image inspect "${BUILDER_IMAGE_REF}" >/dev/null 2>&1; then
  log "builder image ${BUILDER_IMAGE_REF} missing, building it first"
  make -C "${ROOT_DIR}" builder-image BUILDER_IMAGE="${BUILDER_IMAGE_REF}" >&2
fi

log "building one-click component binaries in builder"
make -C "${ROOT_DIR}" builder-run \
  BUILDER_IMAGE="${BUILDER_IMAGE_REF}" \
  BUILDER_CMD="ONE_CLICK_TARGET_ARCH=${TARGET_ARCH} ONE_CLICK_TARGET_PLATFORM=${TARGET_PLATFORM} ONE_CLICK_RUST_MUSL_TRIPLE=${RUST_MUSL_TRIPLE} bash /workspace/deploy/one-click/.work/build-prebuilt-in-builder.sh" >&2

for artifact in \
  cubemaster \
  cubemastercli \
  cubelet \
  cubecli \
  cube-api \
  network-agent \
  cube-agent \
  containerd-shim-cube-rs \
  cube-runtime
do
  ensure_file "${PREBUILT_DIR}/${artifact}"
done

log "packaging one-click release bundle on host with prebuilt artifacts"
ONE_CLICK_TARGET_ARCH="${TARGET_ARCH}" \
ONE_CLICK_CUBEMASTER_BIN="${PREBUILT_DIR}/cubemaster" \
ONE_CLICK_CUBEMASTERCLI_BIN="${PREBUILT_DIR}/cubemastercli" \
ONE_CLICK_CUBELET_BIN="${PREBUILT_DIR}/cubelet" \
ONE_CLICK_CUBECLI_BIN="${PREBUILT_DIR}/cubecli" \
ONE_CLICK_CUBE_API_BIN="${PREBUILT_DIR}/cube-api" \
ONE_CLICK_NETWORK_AGENT_BIN="${PREBUILT_DIR}/network-agent" \
ONE_CLICK_CUBE_AGENT_BIN="${PREBUILT_DIR}/cube-agent" \
ONE_CLICK_CUBESHIM_BIN="${PREBUILT_DIR}/containerd-shim-cube-rs" \
ONE_CLICK_CUBE_RUNTIME_BIN="${PREBUILT_DIR}/cube-runtime" \
  "${SCRIPT_DIR}/build-release-bundle.sh" "$@"
