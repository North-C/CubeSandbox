#!/bin/sh

set -eu

ENVD_BUILD_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
# shellcheck source=../versions.env
. "$ENVD_BUILD_ROOT/versions.env"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

normalize_arch() {
  case "$1" in
    amd64|x86_64) printf '%s\n' amd64 ;;
    arm64|aarch64) printf '%s\n' arm64 ;;
    *) echo "unsupported architecture: $1 (expected amd64 or arm64)" >&2; exit 2 ;;
  esac
}

base_image_for_arch() {
  case "$1" in
    amd64) printf '%s\n' "$SANDBOX_CODE_AMD64" ;;
    arm64) printf '%s\n' "$SANDBOX_CODE_ARM64" ;;
    *) echo "unsupported architecture: $1" >&2; exit 2 ;;
  esac
}

default_tag_for_arch() {
  printf '%s:%s-%s-%s\n' "$DEFAULT_IMAGE_REPOSITORY" envd-mmds-prime "$ENVD_VERSION" "$1"
}

verify_inputs() {
  "$ENVD_BUILD_ROOT/scripts/verify-source-and-patch.sh"
}
