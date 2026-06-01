#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 Tencent. All rights reserved.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common.sh
source "${SCRIPT_DIR}/common.sh"

require_root
require_cmd docker
require_cmd rg
require_cmd sed
require_cmd sha256sum
require_cmd ss
require_cmd tar

CUBE_PROXY_ENABLE="${CUBE_PROXY_ENABLE:-1}"
[[ "${CUBE_PROXY_ENABLE}" == "1" ]] || die "CUBE_PROXY_ENABLE must be 1; cube proxy is required in one-click deployment"

PROXY_DIR="${TOOLBOX_ROOT}/cubeproxy"
BUILD_CONTEXT_DIR="${PROXY_DIR}/build-context"
CUBE_PROXY_CERT_DIR="${CUBE_PROXY_CERT_DIR:-${PROXY_DIR}/certs}"
CERT_DIR="${CUBE_PROXY_CERT_DIR}"
GLOBAL_TEMPLATE="${PROXY_DIR}/global.conf.template"
GLOBAL_CONF="${PROXY_DIR}/global.conf"
CUBE_PROXY_IMAGE_TAG="${CUBE_PROXY_IMAGE_TAG:-cube-proxy:one-click}"
CUBE_PROXY_CONTAINER_NAME="${CUBE_PROXY_CONTAINER_NAME:-cube-proxy}"
CUBE_PROXY_HOST_PORT="${CUBE_PROXY_HOST_PORT:-443}"
CUBE_PROXY_HTTP_HOST_PORT="${CUBE_PROXY_HTTP_HOST_PORT:-80}"
CUBE_PROXY_HTTPS_PORT="${CUBE_PROXY_HTTPS_PORT:-${CUBE_PROXY_HOST_PORT}}"
CUBE_PROXY_HTTP_PORT="${CUBE_PROXY_HTTP_PORT:-${CUBE_PROXY_HTTP_HOST_PORT}}"
CUBE_PROXY_SSL_CERT="${CUBE_PROXY_SSL_CERT:-cube.app+3.pem}"
CUBE_PROXY_SSL_KEY="${CUBE_PROXY_SSL_KEY:-cube.app+3-key.pem}"
CUBE_SANDBOX_NODE_IP="${CUBE_SANDBOX_NODE_IP:-}"
CUBE_PROXY_REDIS_IP="${CUBE_PROXY_REDIS_IP:-${CUBE_SANDBOX_NODE_IP}}"
CUBE_PROXY_REDIS_PORT="${CUBE_PROXY_REDIS_PORT:-${CUBE_SANDBOX_REDIS_PORT:-6379}}"
CUBE_PROXY_REDIS_PASSWORD="${CUBE_PROXY_REDIS_PASSWORD:-${CUBE_SANDBOX_REDIS_PASSWORD:-ceuhvu123}}"
CUBE_PROXY_BASE_IMAGE="${CUBE_PROXY_BASE_IMAGE:-$(default_openresty_image)}"
MKCERT_BUNDLED_BIN="${TOOLBOX_ROOT}/support/bin/mkcert"
BUILD_STAMP_FILE="${PROXY_DIR}/.image-build-stamp"

ensure_dir "${PROXY_DIR}"
ensure_dir "${BUILD_CONTEXT_DIR}"
mkdir -p "${CERT_DIR}"
ensure_file "${BUILD_CONTEXT_DIR}/Dockerfile"
ensure_file "${GLOBAL_TEMPLATE}"
ensure_file "${PROXY_DIR}/nginx.conf.template"
[[ -n "${CUBE_SANDBOX_NODE_IP}" ]] || die "CUBE_SANDBOX_NODE_IP is required for cube proxy"

install_mkcert() {
  local target="/usr/local/bin/mkcert"
  if command -v mkcert >/dev/null 2>&1 && mkcert -version >/dev/null 2>&1; then
    return 0
  fi
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

prepare_proxy_certs() {
  mkdir -p "${CERT_DIR}"
  if [[ -f "${CERT_DIR}/${CUBE_PROXY_SSL_CERT}" && -f "${CERT_DIR}/${CUBE_PROXY_SSL_KEY}" ]]; then
    return 0
  fi

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

build_context_hash() {
  tar -C "${BUILD_CONTEXT_DIR}" -cf - . | sha256sum | awk '{print $1}'
}

prepare_proxy_certs
render_template \
  "${GLOBAL_TEMPLATE}" \
  "${GLOBAL_CONF}" \
  -e "s/__CUBE_PROXY_REDIS_IP__/$(escape_sed "${CUBE_PROXY_REDIS_IP}")/g" \
  -e "s/__CUBE_PROXY_REDIS_PORT__/$(escape_sed "${CUBE_PROXY_REDIS_PORT}")/g" \
  -e "s/__CUBE_PROXY_REDIS_PASSWORD__/$(escape_sed "${CUBE_PROXY_REDIS_PASSWORD}")/g" \
  -e "s/__CUBE_PROXY_HOST_IP__/$(escape_sed "${CUBE_SANDBOX_NODE_IP}")/g"

NGINX_TEMPLATE="${PROXY_DIR}/nginx.conf.template"
NGINX_CONF="${PROXY_DIR}/nginx.conf"
render_template \
  "${NGINX_TEMPLATE}" \
  "${NGINX_CONF}" \
  -e "s/__CUBE_PROXY_HTTPS_PORT__/$(escape_sed "${CUBE_PROXY_HTTPS_PORT}")/g" \
  -e "s/__CUBE_PROXY_HTTP_PORT__/$(escape_sed "${CUBE_PROXY_HTTP_PORT}")/g" \
  -e "s/__CUBE_PROXY_SSL_CERT__/$(escape_sed "${CUBE_PROXY_SSL_CERT}")/g" \
  -e "s/__CUBE_PROXY_SSL_KEY__/$(escape_sed "${CUBE_PROXY_SSL_KEY}")/g"

docker_rm_if_exists "${CUBE_PROXY_CONTAINER_NAME}"
for port in "${CUBE_PROXY_HTTP_PORT}" "${CUBE_PROXY_HTTPS_PORT}"; do
  if ss -lnt "( sport = :${port} )" | rg -q "LISTEN"; then
    die "port ${port} is already in use; cube-proxy requires it to be free"
  fi
done

context_hash="$(build_context_hash)"
if [[ ! -f "${BUILD_STAMP_FILE}" || "$(<"${BUILD_STAMP_FILE}")" != "${CUBE_PROXY_IMAGE_TAG}:${context_hash}" ]] || ! docker_image_exists "${CUBE_PROXY_IMAGE_TAG}"; then
  docker build \
    --build-arg "CUBE_PROXY_BASE_IMAGE=${CUBE_PROXY_BASE_IMAGE}" \
    -t "${CUBE_PROXY_IMAGE_TAG}" \
    "${BUILD_CONTEXT_DIR}" >&2
  printf '%s\n' "${CUBE_PROXY_IMAGE_TAG}:${context_hash}" > "${BUILD_STAMP_FILE}"
fi

docker create \
  --name "${CUBE_PROXY_CONTAINER_NAME}" \
  -p "${CUBE_PROXY_HTTPS_PORT}:${CUBE_PROXY_HTTPS_PORT}" \
  -p "${CUBE_PROXY_HTTP_PORT}:${CUBE_PROXY_HTTP_PORT}" \
  -v "${CERT_DIR}:/usr/local/openresty/nginx/certs:ro" \
  -v "${GLOBAL_CONF}:/usr/local/openresty/nginx/conf/global/global.conf:ro" \
  -v "${NGINX_CONF}:/usr/local/openresty/nginx/conf/nginx.conf:ro" \
  "${CUBE_PROXY_IMAGE_TAG}" >/dev/null

exec docker start -a "${CUBE_PROXY_CONTAINER_NAME}"
