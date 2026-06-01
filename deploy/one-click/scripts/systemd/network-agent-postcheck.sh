#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"
health_addr="${NETWORK_AGENT_HEALTH_ADDR:-127.0.0.1:19090}"
timeout="${NETWORK_AGENT_READY_TIMEOUT:-180}"

wait_for_http "http://${health_addr}/healthz" "${timeout}" 1 || die "network-agent healthz not ready"
wait_for_http "http://${health_addr}/readyz" 10 1 || die "network-agent readyz not ready"
