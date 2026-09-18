#!/bin/sh
set -eu

image="${1:?usage: smoke-image.sh IMAGE [ARCH]}"
arch="${2:-}"
for command_name in docker curl; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command not found: $command_name" >&2
    exit 1
  fi
done

if [ -z "$arch" ]; then
  arch="$(docker image inspect --format '{{.Architecture}}' "$image")"
fi
case "$arch" in amd64|arm64) ;; *) echo "unsupported image architecture: $arch" >&2; exit 2 ;; esac

container_name="envd-mmds-prime-smoke-$$"
cleanup() {
  docker logs "$container_name" >&2 2>/dev/null || true
  docker container rm -f "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

docker run --detach --name "$container_name" --platform "linux/$arch" \
  --publish 127.0.0.1::49983 --publish 127.0.0.1::49999 "$image" >/dev/null

envd_port="$(docker port "$container_name" 49983/tcp | sed -n '1s/.*://p')"
business_port="$(docker port "$container_name" 49999/tcp | sed -n '1s/.*://p')"
if [ -z "$envd_port" ] || [ -z "$business_port" ]; then
  echo "failed to resolve published ports" >&2
  exit 1
fi

attempt=0
until [ "$attempt" -ge 120 ]; do
  if curl --fail --silent --show-error --output /dev/null "http://127.0.0.1:$envd_port/health" \
    && curl --fail --silent --show-error --output /dev/null "http://127.0.0.1:$business_port/health"; then
    docker image inspect --format 'id={{.Id}} os={{.Os}} arch={{.Architecture}}' "$image"
    echo "envd smoke: http://127.0.0.1:$envd_port/health (expected HTTP 204): OK"
    echo "business smoke: http://127.0.0.1:$business_port/health: OK"
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 1
done

echo "smoke test timed out after 120 seconds" >&2
exit 1
