#!/bin/sh
set -eu

# shellcheck source=./common.sh
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/common.sh"

require_command docker

arch="$(normalize_arch "${1:-}")"
tag="${2:-$(default_tag_for_arch "$arch")}"
base_image="$(base_image_for_arch "$arch")"
output_dir="${OUTPUT_DIR:-$ENVD_BUILD_ROOT/_output/images}"
metadata="$output_dir/$arch-metadata.json"
image_archive="$output_dir/$arch-image.tar"

verify_inputs
mkdir -p "$output_dir"

docker buildx build \
  --platform "linux/$arch" \
  --target sandbox-code \
  --no-cache \
  --build-context "envd-deps=$ENVD_BUILD_ROOT/build-context-empty" \
  --build-arg "GO_IMAGE=$GO_IMAGE" \
  --build-arg "BASE_IMAGE=$base_image" \
  --build-arg "ENVD_COMMIT=$ENVD_UPSTREAM_COMMIT" \
  --build-arg "ENVD_PATCH_SHA256=$PATCH_SHA256" \
  --build-arg "SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH" \
  --metadata-file "$metadata" \
  --provenance=false \
  --output "type=docker,dest=$image_archive,rewrite-timestamp=true" \
  --tag "$tag" \
  --progress plain \
  "$ENVD_BUILD_ROOT"

docker image load --input "$image_archive" >/dev/null
actual_arch="$(docker image inspect --format '{{.Architecture}}' "$tag")"
if [ "$actual_arch" != "$arch" ]; then
  echo "image architecture mismatch: expected $arch, got $actual_arch" >&2
  exit 1
fi

docker image inspect --format 'tag={{join .RepoTags ","}} id={{.Id}} os={{.Os}} arch={{.Architecture}}' "$tag"
echo "build metadata: $metadata"
