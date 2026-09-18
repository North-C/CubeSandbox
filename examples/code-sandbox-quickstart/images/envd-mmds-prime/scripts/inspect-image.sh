#!/bin/sh
set -eu

image="${1:?usage: inspect-image.sh IMAGE}"
for command_name in docker file go sha256sum; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command not found: $command_name" >&2
    exit 1
  fi
done

tmp="$(mktemp -d "${TMPDIR:-/tmp}/envd-image-inspect.XXXXXX")"
container_name="envd-image-inspect-$$"
cleanup() {
  docker container rm "$container_name" >/dev/null 2>&1 || true
  rm -rf -- "$tmp"
}
trap cleanup EXIT HUP INT TERM

docker create --name "$container_name" "$image" >/dev/null
docker cp "$container_name:/usr/bin/envd" "$tmp/envd"
docker cp "$container_name:/usr/local/bin/start-lightweight-code-interpreter.sh" "$tmp/start.sh"

docker image inspect --format 'id={{.Id}} os={{.Os}} arch={{.Architecture}} repo_digests={{json .RepoDigests}}' "$image"
file "$tmp/envd"
go version -m "$tmp/envd"
sha256sum "$tmp/envd" "$tmp/start.sh"
