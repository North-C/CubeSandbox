#!/bin/sh
set -eu

# shellcheck source=./common.sh
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/common.sh"

for command_name in docker tar sha256sum; do
  require_command "$command_name"
done

bundle_dir="${1:?usage: build-offline.sh BUNDLE_DIR ARCH [TAG]}"
arch="$(normalize_arch "${2:-}")"
tag="${3:-$(default_tag_for_arch "$arch")-offline}"
bundle_dir="$(CDPATH= cd -- "$bundle_dir" && pwd)"
host_arch="$(normalize_arch "$(uname -m)")"

verify_inputs
(cd "$bundle_dir" && sha256sum -c SHA256SUMS)
grep -Fx 'BUNDLE_FORMAT=1' "$bundle_dir/OFFLINE.env" >/dev/null
grep -Fx "HOST_ARCH=$host_arch" "$bundle_dir/OFFLINE.env" >/dev/null
grep -Fx "ENVD_UPSTREAM_COMMIT=$ENVD_UPSTREAM_COMMIT" "$bundle_dir/OFFLINE.env" >/dev/null
grep -Fx "PATCH_SHA256=$PATCH_SHA256" "$bundle_dir/OFFLINE.env" >/dev/null
grep -Fx "SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH" "$bundle_dir/OFFLINE.env" >/dev/null

docker load --input "$bundle_dir/go-builder.tar" >/dev/null
docker load --input "$bundle_dir/sandbox-code-$arch.tar" >/dev/null

tmp="$(mktemp -d "${TMPDIR:-/tmp}/envd-offline-build.XXXXXX")"
cleanup() {
  rm -rf -- "$tmp"
}
trap cleanup EXIT HUP INT TERM
tar -xzf "$bundle_dir/vendor.tar.gz" -C "$tmp"

go_offline_tag="cubesandbox-envd-offline/go:$GO_VERSION-$host_arch"
base_offline_tag="cubesandbox-envd-offline/sandbox-code:$arch"
output_dir="${OUTPUT_DIR:-$ENVD_BUILD_ROOT/_output/offline-images}"
mkdir -p "$output_dir"
image_archive="$output_dir/$arch-image.tar"

docker buildx build \
  --platform "linux/$arch" \
  --target sandbox-code \
  --network none \
  --no-cache \
  --pull=false \
  --build-context "envd-deps=$tmp/vendor" \
  --build-arg "GO_IMAGE=$go_offline_tag" \
  --build-arg "BASE_IMAGE=$base_offline_tag" \
  --build-arg "GOFLAGS=-mod=vendor" \
  --build-arg "ENVD_COMMIT=$ENVD_UPSTREAM_COMMIT" \
  --build-arg "ENVD_PATCH_SHA256=$PATCH_SHA256" \
  --build-arg "SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH" \
  --metadata-file "$output_dir/$arch-metadata.json" \
  --provenance=false \
  --output "type=docker,dest=$image_archive,rewrite-timestamp=true" \
  --tag "$tag" \
  --progress plain \
  "$ENVD_BUILD_ROOT"

docker image load --input "$image_archive" >/dev/null
actual_arch="$(docker image inspect --format '{{.Architecture}}' "$tag")"
test "$actual_arch" = "$arch"
docker image inspect --format 'tag={{join .RepoTags ","}} id={{.Id}} os={{.Os}} arch={{.Architecture}}' "$tag"
