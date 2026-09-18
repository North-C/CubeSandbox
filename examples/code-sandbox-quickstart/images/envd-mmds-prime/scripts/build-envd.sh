#!/bin/sh
set -eu

# shellcheck source=./common.sh
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/common.sh"

require_command docker
require_command file
require_command go

arch="$(normalize_arch "${1:-}")"
output_root="${OUTPUT_DIR:-$ENVD_BUILD_ROOT/_output/envd}"
output_dir="$output_root/$arch"
metadata="$output_root/$arch-metadata.json"

verify_inputs
mkdir -p "$output_dir"

docker buildx build \
  --platform "linux/$arch" \
  --target envd-artifact \
  --no-cache \
  --build-context "envd-deps=$ENVD_BUILD_ROOT/build-context-empty" \
  --build-arg "GO_IMAGE=$GO_IMAGE" \
  --build-arg "ENVD_COMMIT=$ENVD_UPSTREAM_COMMIT" \
  --build-arg "SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH" \
  --output "type=local,dest=$output_dir" \
  --metadata-file "$metadata" \
  --provenance=false \
  --progress plain \
  "$ENVD_BUILD_ROOT"

file_output="$(file "$output_dir/envd")"
build_info="$(go version -m "$output_dir/envd")"
printf '%s\n' "$file_output"
printf '%s\n' "$build_info"
printf '%s\n' "$build_info" | grep -F "GOARCH=$arch" >/dev/null
case "$arch:$file_output" in
  amd64:*x86-64*) ;;
  arm64:*ARM\ aarch64*) ;;
  *) echo "ELF architecture mismatch for $arch: $file_output" >&2; exit 1 ;;
esac
sha256sum "$output_dir/envd"
