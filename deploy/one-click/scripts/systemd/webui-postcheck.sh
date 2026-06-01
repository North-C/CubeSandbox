#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"
if [[ "${WEB_UI_ENABLE:-1}" != "1" ]]; then
  exit 0
fi

container="${WEB_UI_CONTAINER_NAME:-cube-webui}"
port="${WEB_UI_HOST_PORT:-12088}"
timeout="${WEB_UI_READY_TIMEOUT:-120}"

wait_for_container_health "${container}" "${timeout}" 1 || die "webui container not ready"
wait_for_http "http://127.0.0.1:${port}/" "${timeout}" 1 || die "webui index not ready"
wait_for_http "http://127.0.0.1:${port}/cubeapi/v1/health" "${timeout}" 1 || die "webui cube-api route not ready"
