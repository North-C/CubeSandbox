#!/bin/sh
set -eu

# shellcheck source=./common.sh
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/common.sh"

require_command docker
verify_inputs

docker buildx build \
  --platform linux/amd64 \
  --target envd-test \
  --build-context "envd-deps=$ENVD_BUILD_ROOT/build-context-empty" \
  --build-arg "GO_IMAGE=$GO_IMAGE" \
  --build-arg "ENVD_COMMIT=$ENVD_UPSTREAM_COMMIT" \
  --output type=cacheonly \
  --progress plain \
  "$ENVD_BUILD_ROOT"
