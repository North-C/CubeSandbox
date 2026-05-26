#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common.sh
source "${SCRIPT_DIR}/common.sh"
# shellcheck source=./compose-lib.sh
source "${SCRIPT_DIR}/compose-lib.sh"

require_root
require_cmd docker
require_cmd sed
require_cmd ss

CUBE_PROXY_ENABLE="${CUBE_PROXY_ENABLE:-1}"
[[ "${CUBE_PROXY_ENABLE}" == "1" ]] || die "CUBE_PROXY_ENABLE must be 1; cube proxy is required in one-click deployment"

PROXY_DIR="${TOOLBOX_ROOT}/cubeproxy"
BUILD_CONTEXT_DIR="${PROXY_DIR}/build-context"
CUBE_PROXY_CERT_DIR="${CUBE_PROXY_CERT_DIR:-${PROXY_DIR}/certs}"
CERT_DIR="${CUBE_PROXY_CERT_DIR}"
GLOBAL_TEMPLATE="${PROXY_DIR}/global.conf.template"
GLOBAL_CONF="${PROXY_DIR}/global.conf"
COMPOSE_TEMPLATE="${PROXY_DIR}/docker-compose.yaml.template"
COMPOSE_FILE="${PROXY_DIR}/docker-compose.yaml"

CUBE_PROXY_IMAGE_TAG="${CUBE_PROXY_IMAGE_TAG:-cube-proxy:one-click}"
CUBE_PROXY_CONTAINER_NAME="${CUBE_PROXY_CONTAINER_NAME:-cube-proxy}"
CUBE_SANDBOX_NODE_IP="${CUBE_SANDBOX_NODE_IP:-}"
CUBE_PROXY_REDIS_IP="${CUBE_PROXY_REDIS_IP:-127.0.0.1}"
CUBE_PROXY_REDIS_PORT="${CUBE_PROXY_REDIS_PORT:-${CUBE_SANDBOX_REDIS_PORT:-6379}}"
CUBE_PROXY_REDIS_PASSWORD="${CUBE_PROXY_REDIS_PASSWORD:-${CUBE_SANDBOX_REDIS_PASSWORD:-ceuhvu123}}"
CUBE_PROXY_HTTPS_PORT="${CUBE_PROXY_HTTPS_PORT:-443}"
CUBE_PROXY_HTTP_PORT="${CUBE_PROXY_HTTP_PORT:-80}"
CUBE_PROXY_SSL_CERT="${CUBE_PROXY_SSL_CERT:-cube.app+3.pem}"
CUBE_PROXY_SSL_KEY="${CUBE_PROXY_SSL_KEY:-cube.app+3-key.pem}"
MKCERT_BUNDLED_BIN="${TOOLBOX_ROOT}/support/bin/mkcert"

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

proxy_target_arch() {
  if [[ -n "${ONE_CLICK_TARGET_ARCH:-}" ]]; then
    normalize_one_click_arch "${ONE_CLICK_TARGET_ARCH}"
    return 0
  fi

  detect_host_one_click_arch
}

default_cube_proxy_base_image() {
  case "$(proxy_target_arch)" in
    amd64) printf 'cube-sandbox-image.tencentcloudcr.com/opensource/openresty:1.21.4.1-6-alpine-fat\n' ;;
    arm64) printf 'openresty/openresty:1.21.4.1-6-alpine-fat\n' ;;
  esac
}

CUBE_PROXY_BASE_IMAGE="${CUBE_PROXY_BASE_IMAGE:-$(default_cube_proxy_base_image)}"

ensure_dir "${PROXY_DIR}"
ensure_dir "${BUILD_CONTEXT_DIR}"
mkdir -p "${CERT_DIR}"
ensure_file "${BUILD_CONTEXT_DIR}/Dockerfile"
ensure_file "${GLOBAL_TEMPLATE}"
ensure_file "${COMPOSE_TEMPLATE}"
[[ -n "${CUBE_SANDBOX_NODE_IP}" ]] || die "CUBE_SANDBOX_NODE_IP is required for cube proxy"

install_mkcert() {
  if command -v mkcert >/dev/null 2>&1 && mkcert -version >/dev/null 2>&1; then
    return 0
  fi

  local target="/usr/local/bin/mkcert"
  if [[ -x "${MKCERT_BUNDLED_BIN}" ]] && "${MKCERT_BUNDLED_BIN}" -version >/dev/null 2>&1; then
    install -m 0755 "${MKCERT_BUNDLED_BIN}" "${target}"
  else
    return 1
  fi

  command -v mkcert >/dev/null 2>&1 && mkcert -version >/dev/null 2>&1
}

prepare_proxy_certs_with_openssl() {
  require_cmd openssl

  local openssl_conf="${CERT_DIR}/cube.app.openssl.cnf"
  cat > "${openssl_conf}" <<'EOF'
[req]
default_bits = 2048
prompt = no
default_md = sha256
distinguished_name = dn
x509_extensions = v3_req

[dn]
CN = cube.app

[v3_req]
subjectAltName = @alt_names

[alt_names]
DNS.1 = cube.app
DNS.2 = *.cube.app
DNS.3 = localhost
IP.1 = 127.0.0.1
EOF

  openssl req \
    -x509 \
    -nodes \
    -newkey rsa:2048 \
    -days 3650 \
    -keyout "${CERT_DIR}/cube.app+3-key.pem" \
    -out "${CERT_DIR}/cube.app+3.pem" \
    -config "${openssl_conf}" \
    >/dev/null 2>&1
}

escape_sed() {
  printf '%s' "$1" | sed 's/[\/&]/\\&/g'
}

prepare_proxy_certs() {
  mkdir -p "${CERT_DIR}"
  if [[ -f "${CERT_DIR}/${CUBE_PROXY_SSL_CERT}" && -f "${CERT_DIR}/${CUBE_PROXY_SSL_KEY}" ]]; then
    return 0
  fi

  # Only auto-generate when using mkcert's default file naming. If the user
  # overrode CUBE_PROXY_SSL_CERT/KEY, they are expected to provision the cert
  # files themselves under CERT_DIR.
  if [[ "${CUBE_PROXY_SSL_CERT}" != "cube.app+3.pem" || "${CUBE_PROXY_SSL_KEY}" != "cube.app+3-key.pem" ]]; then
    die "TLS cert/key not found at ${CERT_DIR}/${CUBE_PROXY_SSL_CERT} or ${CERT_DIR}/${CUBE_PROXY_SSL_KEY}; place them manually when overriding CUBE_PROXY_SSL_CERT/KEY"
  fi

  if install_mkcert; then
    (
      cd "${CERT_DIR}"
      mkcert -install
      mkcert cube.app "*.cube.app" localhost 127.0.0.1
    ) >&2
  else
    log "runnable mkcert not available; generating local cube proxy certificate with openssl"
    prepare_proxy_certs_with_openssl
  fi
}

prepare_proxy_certs

sed \
  -e "s/__CUBE_PROXY_REDIS_IP__/$(escape_sed "${CUBE_PROXY_REDIS_IP}")/g" \
  -e "s/__CUBE_PROXY_REDIS_PORT__/$(escape_sed "${CUBE_PROXY_REDIS_PORT}")/g" \
  -e "s/__CUBE_PROXY_REDIS_PASSWORD__/$(escape_sed "${CUBE_PROXY_REDIS_PASSWORD}")/g" \
  -e "s/__CUBE_PROXY_HOST_IP__/$(escape_sed "${CUBE_SANDBOX_NODE_IP}")/g" \
  "${GLOBAL_TEMPLATE}" > "${GLOBAL_CONF}"

NGINX_TEMPLATE="${PROXY_DIR}/nginx.conf.template"
NGINX_CONF="${PROXY_DIR}/nginx.conf"
ensure_file "${NGINX_TEMPLATE}"
sed \
  -e "s/__CUBE_PROXY_HTTPS_PORT__/$(escape_sed "${CUBE_PROXY_HTTPS_PORT}")/g" \
  -e "s/__CUBE_PROXY_HTTP_PORT__/$(escape_sed "${CUBE_PROXY_HTTP_PORT}")/g" \
  -e "s/__CUBE_PROXY_SSL_CERT__/$(escape_sed "${CUBE_PROXY_SSL_CERT}")/g" \
  -e "s/__CUBE_PROXY_SSL_KEY__/$(escape_sed "${CUBE_PROXY_SSL_KEY}")/g" \
  "${NGINX_TEMPLATE}" > "${NGINX_CONF}"

sed \
  -e "s#__CUBE_PROXY_IMAGE__#$(escape_sed "${CUBE_PROXY_IMAGE_TAG}")#g" \
  -e "s#__CUBE_PROXY_BASE_IMAGE__#$(escape_sed "${CUBE_PROXY_BASE_IMAGE}")#g" \
  -e "s#__CUBE_PROXY_CONTAINER_NAME__#$(escape_sed "${CUBE_PROXY_CONTAINER_NAME}")#g" \
  -e "s#__CUBE_PROXY_BUILD_CONTEXT__#$(escape_sed "${BUILD_CONTEXT_DIR}")#g" \
  -e "s#__CUBE_PROXY_CERT_DIR__#$(escape_sed "${CERT_DIR}")#g" \
  -e "s#__CUBE_PROXY_GLOBAL_CONF__#$(escape_sed "${GLOBAL_CONF}")#g" \
  -e "s#__CUBE_PROXY_NGINX_CONF__#$(escape_sed "${NGINX_CONF}")#g" \
  "${COMPOSE_TEMPLATE}" > "${COMPOSE_FILE}"

compose_run down --remove-orphans >/dev/null 2>&1 || true

# cube-proxy uses network_mode: host, so HTTP/HTTPS ports must be free on the
# host before we attempt to start the container; otherwise the failure mode is
# a cryptic "address already in use" from nginx inside the container.
for port in "${CUBE_PROXY_HTTP_PORT}" "${CUBE_PROXY_HTTPS_PORT}"; do
  if ss -lnt "( sport = :${port} )" | rg -q "LISTEN"; then
    die "port ${port} is already in use; cube-proxy uses host networking and requires it to be free"
  fi
done

compose_run build cube-proxy
compose_run up -d cube-proxy

for _ in {1..40}; do
  state="$(docker inspect --format '{{.State.Status}}' "${CUBE_PROXY_CONTAINER_NAME}" 2>/dev/null || true)"
  if [[ "${state}" == "running" ]]; then
    break
  fi
  sleep 2
done
[[ "${state:-}" == "running" ]] || die "cube proxy container failed to start"

http_ready=0
https_ready=0
for _ in {1..30}; do
  if [[ "${http_ready}" == "0" ]] && \
     ss -lnt "( sport = :${CUBE_PROXY_HTTP_PORT} )" | rg -q "LISTEN"; then
    http_ready=1
  fi
  if [[ "${https_ready}" == "0" ]] && \
     ss -lnt "( sport = :${CUBE_PROXY_HTTPS_PORT} )" | rg -q "LISTEN"; then
    https_ready=1
  fi
  if [[ "${http_ready}" == "1" && "${https_ready}" == "1" ]]; then
    log "cube proxy listening on ${CUBE_PROXY_HTTP_PORT} and ${CUBE_PROXY_HTTPS_PORT}"
    exit 0
  fi
  sleep 2
done

if [[ "${http_ready}" != "1" ]]; then
  die "cube proxy port ${CUBE_PROXY_HTTP_PORT} (HTTP) did not become ready"
fi
die "cube proxy port ${CUBE_PROXY_HTTPS_PORT} (HTTPS) did not become ready"
