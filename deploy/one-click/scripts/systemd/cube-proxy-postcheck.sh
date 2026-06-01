#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"
container="${CUBE_PROXY_CONTAINER_NAME:-cube-proxy}"
timeout="${CUBE_PROXY_READY_TIMEOUT:-180}"

wait_for_container_health "${container}" "${timeout}" 1 || die "cube-proxy container not ready"
