#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common.sh
source "${SCRIPT_DIR}/common.sh"
# shellcheck source=./support-compose-lib.sh
source "${SCRIPT_DIR}/support-compose-lib.sh"

require_root
require_cmd docker
require_cmd sed

MYSQL_CONTAINER="${CUBE_SANDBOX_MYSQL_CONTAINER:-cube-sandbox-mysql}"
REDIS_CONTAINER="${CUBE_SANDBOX_REDIS_CONTAINER:-cube-sandbox-redis}"
MYSQL_VOLUME="${CUBE_SANDBOX_MYSQL_VOLUME:-cube-sandbox-mysql-data}"
REDIS_VOLUME="${CUBE_SANDBOX_REDIS_VOLUME:-cube-sandbox-redis-data}"
MYSQL_PORT="${CUBE_SANDBOX_MYSQL_PORT:-3306}"
REDIS_PORT="${CUBE_SANDBOX_REDIS_PORT:-6379}"
REDIS_PASSWORD="${CUBE_SANDBOX_REDIS_PASSWORD:-ceuhvu123}"
MYSQL_DB="${CUBE_SANDBOX_MYSQL_DB:-cube_mvp}"
MYSQL_USER="${CUBE_SANDBOX_MYSQL_USER:-cube}"
MYSQL_PASSWORD="${CUBE_SANDBOX_MYSQL_PASSWORD:-cube_pass}"
MYSQL_ROOT_PASSWORD="${CUBE_SANDBOX_MYSQL_ROOT_PASSWORD:-cube_root}"
SQL_DIR="${TOOLBOX_ROOT}/sql"
SUPPORT_DIR="${TOOLBOX_ROOT}/support"
SUPPORT_TEMPLATE="${SUPPORT_DIR}/docker-compose.yaml.template"
SUPPORT_COMPOSE_FILE="${SUPPORT_DIR}/docker-compose.yaml"

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

support_target_arch() {
  if [[ -n "${ONE_CLICK_TARGET_ARCH:-}" ]]; then
    normalize_one_click_arch "${ONE_CLICK_TARGET_ARCH}"
    return 0
  fi

  detect_host_one_click_arch
}

default_support_mysql_image() {
  case "$(support_target_arch)" in
    amd64) printf 'cube-sandbox-image.tencentcloudcr.com/opensource/mysql:8.0\n' ;;
    arm64) printf 'mysql:8.0\n' ;;
  esac
}

default_support_redis_image() {
  case "$(support_target_arch)" in
    amd64) printf 'cube-sandbox-image.tencentcloudcr.com/opensource/redis:7-alpine\n' ;;
    arm64) printf 'redis:7-alpine\n' ;;
  esac
}

MYSQL_IMAGE="${CUBE_SANDBOX_MYSQL_IMAGE:-$(default_support_mysql_image)}"
REDIS_IMAGE="${CUBE_SANDBOX_REDIS_IMAGE:-$(default_support_redis_image)}"

ensure_dir "${SUPPORT_DIR}"
ensure_dir "${SQL_DIR}"
ensure_file "${SUPPORT_TEMPLATE}"

escape_sed() {
  printf '%s' "$1" | sed 's/[\/&]/\\&/g'
}

sed \
  -e "s/__MYSQL_IMAGE__/$(escape_sed "${MYSQL_IMAGE}")/g" \
  -e "s/__REDIS_IMAGE__/$(escape_sed "${REDIS_IMAGE}")/g" \
  -e "s/__MYSQL_CONTAINER__/$(escape_sed "${MYSQL_CONTAINER}")/g" \
  -e "s/__REDIS_CONTAINER__/$(escape_sed "${REDIS_CONTAINER}")/g" \
  -e "s/__MYSQL_VOLUME__/$(escape_sed "${MYSQL_VOLUME}")/g" \
  -e "s/__REDIS_VOLUME__/$(escape_sed "${REDIS_VOLUME}")/g" \
  -e "s/__MYSQL_PORT__/$(escape_sed "${MYSQL_PORT}")/g" \
  -e "s/__REDIS_PORT__/$(escape_sed "${REDIS_PORT}")/g" \
  -e "s/__REDIS_PASSWORD__/$(escape_sed "${REDIS_PASSWORD}")/g" \
  -e "s/__MYSQL_DB__/$(escape_sed "${MYSQL_DB}")/g" \
  -e "s/__MYSQL_USER__/$(escape_sed "${MYSQL_USER}")/g" \
  -e "s/__MYSQL_PASSWORD__/$(escape_sed "${MYSQL_PASSWORD}")/g" \
  -e "s/__MYSQL_ROOT_PASSWORD__/$(escape_sed "${MYSQL_ROOT_PASSWORD}")/g" \
  -e "s#__SQL_DIR__#$(escape_sed "${SQL_DIR}")#g" \
  "${SUPPORT_TEMPLATE}" > "${SUPPORT_COMPOSE_FILE}"

support_compose_run down --remove-orphans >/dev/null 2>&1 || true
support_compose_run up -d

wait_for_health "${MYSQL_CONTAINER}" || die "mysql container did not become healthy"
wait_for_health "${REDIS_CONTAINER}" || die "redis container did not become healthy"

log "support services ready under ${SUPPORT_DIR}"
