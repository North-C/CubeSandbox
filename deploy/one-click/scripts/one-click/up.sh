#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common.sh
source "${SCRIPT_DIR}/common.sh"

TOOLBOX_ROOT="${ONE_CLICK_TOOLBOX_ROOT:-/usr/local/services/cubetoolbox}"

NETWORK_AGENT_BIN="${TOOLBOX_ROOT}/network-agent/bin/network-agent"
NETWORK_AGENT_CFG="${TOOLBOX_ROOT}/network-agent/network-agent.yaml"
NETWORK_AGENT_STATE_DIR="${TOOLBOX_ROOT}/network-agent/state"
NETWORK_AGENT_HEALTH_ADDR="${NETWORK_AGENT_HEALTH_ADDR:-127.0.0.1:19090}"
NETWORK_AGENT_READY_TIMEOUT="${NETWORK_AGENT_READY_TIMEOUT:-120}"
CUBE_API_BIN="${TOOLBOX_ROOT}/CubeAPI/bin/cube-api"
CUBE_API_LOG_DIR="${CUBE_API_LOG_DIR:-/data/log/CubeAPI}"
CUBE_API_HEALTH_ADDR="${CUBE_API_HEALTH_ADDR:-127.0.0.1:3000}"
CUBEMASTER_BIN="${TOOLBOX_ROOT}/CubeMaster/bin/cubemaster"
CUBEMASTER_CFG="${TOOLBOX_ROOT}/CubeMaster/conf.yaml"
CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR_DEFAULT="/data/CubeMaster/storage"
CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR_CONFIGURED="${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR:-}"
CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR="${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR_CONFIGURED:-${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR_DEFAULT}}"
CUBELET_BIN="${TOOLBOX_ROOT}/Cubelet/bin/cubelet"
CUBELET_CONFIG="${TOOLBOX_ROOT}/Cubelet/config/config.toml"
CUBELET_DYNAMICCONF="${TOOLBOX_ROOT}/Cubelet/dynamicconf/conf.yaml"
CUBE_API_OPTIONAL_EXPORTS=""
CUBELET_OPTIONAL_EXPORTS=""

require_cmd bash
require_cmd curl
require_cmd awk

test -x "${NETWORK_AGENT_BIN}" || die "network-agent binary missing: ${NETWORK_AGENT_BIN}"
test -x "${CUBE_API_BIN}" || die "cube-api binary missing: ${CUBE_API_BIN}"
test -x "${CUBEMASTER_BIN}" || die "cubemaster binary missing: ${CUBEMASTER_BIN}"
test -x "${CUBELET_BIN}" || die "cubelet binary missing: ${CUBELET_BIN}"
test -f "${NETWORK_AGENT_CFG}" || die "network-agent config missing: ${NETWORK_AGENT_CFG}"
test -f "${CUBEMASTER_CFG}" || die "cubemaster config missing: ${CUBEMASTER_CFG}"
test -f "${CUBELET_CONFIG}" || die "cubelet config missing: ${CUBELET_CONFIG}"
test -f "${CUBELET_DYNAMICCONF}" || die "cubelet dynamic config missing: ${CUBELET_DYNAMICCONF}"

mkdir -p "${NETWORK_AGENT_STATE_DIR}" "${CUBE_API_LOG_DIR}" /tmp/cube

yaml_quote() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '"%s"' "${value}"
}

configure_cubemaster_runtime_config() {
  local mysql_addr="${CUBEMASTER_MYSQL_ADDR:-127.0.0.1:${CUBE_SANDBOX_MYSQL_PORT:-3306}}"
  local mysql_user="${CUBEMASTER_MYSQL_USER:-${CUBE_SANDBOX_MYSQL_USER:-cube}}"
  local mysql_pwd="${CUBEMASTER_MYSQL_PASSWORD:-${CUBE_SANDBOX_MYSQL_PASSWORD:-cube_pass}}"
  local mysql_db="${CUBEMASTER_MYSQL_DB:-${CUBE_SANDBOX_MYSQL_DB:-cube_mvp}}"
  local redis_addr="${CUBEMASTER_REDIS_ADDR:-127.0.0.1:${CUBE_SANDBOX_REDIS_PORT:-6379}}"
  local redis_pwd="${CUBEMASTER_REDIS_PASSWORD:-${CUBE_SANDBOX_REDIS_PASSWORD:-ceuhvu123}}"
  local tmp_cfg

  tmp_cfg="${CUBEMASTER_CFG}.tmp.$$"
  awk \
    -v mysql_addr="$(yaml_quote "${mysql_addr}")" \
    -v mysql_user="$(yaml_quote "${mysql_user}")" \
    -v mysql_pwd="$(yaml_quote "${mysql_pwd}")" \
    -v mysql_db="$(yaml_quote "${mysql_db}")" \
    -v redis_addr="$(yaml_quote "${redis_addr}")" \
    -v redis_pwd="$(yaml_quote "${redis_pwd}")" \
    '
      /^[A-Za-z0-9_]+:[[:space:]]*$/ {
        section = $1
        sub(/:$/, "", section)
      }
      section == "ossdb_config" || section == "instance_db_config" {
        if ($1 == "addr:") { print "  addr: " mysql_addr; next }
        if ($1 == "user:") { print "  user: " mysql_user; next }
        if ($1 == "pwd:") { print "  pwd: " mysql_pwd; next }
        if ($1 == "db_name:") { print "  db_name: " mysql_db; next }
      }
      section == "redis" || section == "redis_read" || section == "redis_write" {
        if ($1 == "nodes:") { print "  nodes: " redis_addr; next }
        if ($1 == "password:") { print "  password: " redis_pwd; next }
      }
      { print }
    ' "${CUBEMASTER_CFG}" > "${tmp_cfg}"
  mv -f "${tmp_cfg}" "${CUBEMASTER_CFG}"
  log "configured cubemaster stores: mysql=${mysql_addr}, redis=${redis_addr}"
}

CUBEMASTER_ARTIFACT_STORE_EXPORT=""
if [[ -n "${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR_CONFIGURED}" ]]; then
  mkdir -p "${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR}"
  CUBEMASTER_ARTIFACT_STORE_EXPORT="export CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR=\"${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR}\";"
elif mkdir -p "${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR}" >/dev/null 2>&1; then
  CUBEMASTER_ARTIFACT_STORE_EXPORT="export CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR=\"${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR}\";"
else
  log "cubemaster artifact store ${CUBEMASTER_ROOTFS_ARTIFACT_STORE_DIR} unavailable, fallback handled by cubemaster"
fi

if [[ -n "${CUBE_MASTER_ADDR:-}" ]]; then
  CUBE_API_OPTIONAL_EXPORTS+="export CUBE_MASTER_ADDR=\"${CUBE_MASTER_ADDR}\"; "
fi
if [[ -n "${AUTH_CALLBACK_URL:-}" ]]; then
  CUBE_API_OPTIONAL_EXPORTS+="export AUTH_CALLBACK_URL=\"${AUTH_CALLBACK_URL}\"; "
fi
if [[ -n "${CUBE_SANDBOX_NODE_IP:-}" ]]; then
  CUBELET_OPTIONAL_EXPORTS+="export CUBE_SANDBOX_NODE_IP=\"${CUBE_SANDBOX_NODE_IP}\"; "
fi

configure_cubemaster_runtime_config

"${SCRIPT_DIR}/down-local.sh" >/dev/null 2>&1 || true

start_with_pidfile \
  "network-agent" \
  "mkdir -p /tmp/cube \"${NETWORK_AGENT_STATE_DIR}\" && \"${NETWORK_AGENT_BIN}\" --cubelet-config \"${CUBELET_CONFIG}\" --state-dir \"${NETWORK_AGENT_STATE_DIR}\" --health-listen \"${NETWORK_AGENT_HEALTH_ADDR}\""

wait_for_network_agent_ready "${NETWORK_AGENT_HEALTH_ADDR}" "${NETWORK_AGENT_READY_TIMEOUT}" 1 || \
  die "network-agent did not become ready, check logs under ${LOG_DIR}"

start_with_pidfile \
  "cubemaster" \
  "export CUBE_MASTER_CONFIG_PATH=\"${CUBEMASTER_CFG}\"; ${CUBEMASTER_ARTIFACT_STORE_EXPORT} \"${CUBEMASTER_BIN}\""

start_with_pidfile \
  "cube-api" \
  "export LOG_DIR=\"${CUBE_API_LOG_DIR}\" CUBE_API_BIND=\"${CUBE_API_BIND:-0.0.0.0:3000}\" CUBE_API_SANDBOX_DOMAIN=\"${CUBE_API_SANDBOX_DOMAIN:-cube.app}\"; ${CUBE_API_OPTIONAL_EXPORTS}\"${CUBE_API_BIN}\""

start_with_pidfile \
  "cubelet" \
  "${CUBELET_OPTIONAL_EXPORTS}\"${CUBELET_BIN}\" --config \"${CUBELET_CONFIG}\" --dynamic-conf-path \"${CUBELET_DYNAMICCONF}\""
refresh_pidfile_from_pattern "cubelet" "^${CUBELET_BIN} --config" 10 1 || log "cubelet pidfile refresh skipped"

wait_for_http "http://${CUBE_API_HEALTH_ADDR}/health" 30 1 || die "cube-api did not become ready, check logs under ${LOG_DIR}"

for _ in {1..30}; do
  if "${SCRIPT_DIR}/quickcheck.sh" >/dev/null 2>&1; then
    "${SCRIPT_DIR}/quickcheck.sh"
    log "core services ready"
    exit 0
  fi
  sleep 2
done

die "core services did not become ready, check logs under ${LOG_DIR}"
