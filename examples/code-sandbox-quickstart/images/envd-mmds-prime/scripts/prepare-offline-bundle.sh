#!/bin/sh
set -eu

# shellcheck source=./common.sh
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/common.sh"

for command_name in docker tar gzip sha256sum; do
  require_command "$command_name"
done

output_dir="${1:?usage: prepare-offline-bundle.sh OUTPUT_DIR}"
mkdir -p "$output_dir"
if [ -n "$(find "$output_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
  echo "output directory must be empty: $output_dir" >&2
  exit 1
fi
output_dir="$(CDPATH= cd -- "$output_dir" && pwd)"

host_arch="$(normalize_arch "$(uname -m)")"
go_offline_tag="cubesandbox-envd-offline/go:$GO_VERSION-$host_arch"
amd64_offline_tag="cubesandbox-envd-offline/sandbox-code:amd64"
arm64_offline_tag="cubesandbox-envd-offline/sandbox-code:arm64"

verify_inputs

docker pull --platform "linux/$host_arch" "$GO_IMAGE"
docker image tag "$GO_IMAGE" "$go_offline_tag"
docker image save --output "$output_dir/go-builder.tar" "$go_offline_tag"

docker pull --platform linux/amd64 "$SANDBOX_CODE_AMD64"
docker image tag "$SANDBOX_CODE_AMD64" "$amd64_offline_tag"
docker image save --output "$output_dir/sandbox-code-amd64.tar" "$amd64_offline_tag"

docker pull --platform linux/arm64 "$SANDBOX_CODE_ARM64"
docker image tag "$SANDBOX_CODE_ARM64" "$arm64_offline_tag"
docker image save --output "$output_dir/sandbox-code-arm64.tar" "$arm64_offline_tag"

mkdir -p "$output_dir/vendor"
docker run --rm --platform "linux/$host_arch" \
  --user "$(id -u):$(id -g)" \
  --volume "$ENVD_BUILD_ROOT/source:/src/infra:ro" \
  --volume "$output_dir/vendor:/bundle/vendor" \
  --env GOWORK=off \
  --env GOCACHE=/tmp/go-build \
  --env GOMODCACHE=/tmp/go-mod \
  "$GO_IMAGE" \
  sh -euc 'cp -a /src/infra /tmp/infra && cd /tmp/infra/packages/envd && go mod vendor && cp -a vendor/. /bundle/vendor/'

(cd "$output_dir" && tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner -cf - vendor | gzip -n > vendor.tar.gz)
rm -rf -- "$output_dir/vendor"

{
  printf 'BUNDLE_FORMAT=1\n'
  printf 'HOST_ARCH=%s\n' "$host_arch"
  printf 'ENVD_UPSTREAM_COMMIT=%s\n' "$ENVD_UPSTREAM_COMMIT"
  printf 'PATCH_SHA256=%s\n' "$PATCH_SHA256"
  printf 'SOURCE_DATE_EPOCH=%s\n' "$SOURCE_DATE_EPOCH"
  printf 'GO_IMAGE=%s\n' "$GO_IMAGE"
  printf 'GO_OFFLINE_TAG=%s\n' "$go_offline_tag"
  printf 'SANDBOX_CODE_AMD64=%s\n' "$SANDBOX_CODE_AMD64"
  printf 'SANDBOX_CODE_ARM64=%s\n' "$SANDBOX_CODE_ARM64"
} > "$output_dir/OFFLINE.env"

(cd "$output_dir" && sha256sum OFFLINE.env go-builder.tar sandbox-code-amd64.tar sandbox-code-arm64.tar vendor.tar.gz > SHA256SUMS)
echo "offline bundle prepared at $output_dir"
