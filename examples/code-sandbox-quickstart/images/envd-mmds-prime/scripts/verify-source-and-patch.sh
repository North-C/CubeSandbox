#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
# shellcheck source=../versions.env
. "$ROOT/versions.env"

cd "$ROOT"
sha256sum --quiet -c SOURCE_SHA256SUMS

actual_patch_sha="$(sha256sum patches/0001-envd-bound-mmds-prime-window.patch | awk '{print $1}')"
if [ "$actual_patch_sha" != "$PATCH_SHA256" ]; then
  echo "patch checksum mismatch: expected $PATCH_SHA256, got $actual_patch_sha" >&2
  exit 1
fi

tmp="$(mktemp -d "${TMPDIR:-/tmp}/envd-patch-check.XXXXXX")"
cleanup() {
  rm -rf -- "$tmp"
}
trap cleanup EXIT HUP INT TERM

cp -a source/LICENSE source/packages "$tmp/"
git -C "$tmp" apply --check "$ROOT/patches/0001-envd-bound-mmds-prime-window.patch"
git -C "$tmp" apply "$ROOT/patches/0001-envd-bound-mmds-prime-window.patch"

if ! grep -q 'prime-mmds-until-unix' "$tmp/packages/envd/main.go"; then
  echo "patched source does not contain -prime-mmds-until-unix" >&2
  exit 1
fi
if ! grep -q 'disablePostInitMMDSPolling' "$tmp/packages/envd/internal/api/init.go"; then
  echo "patched source does not independently gate PostInit MMDS polling" >&2
  exit 1
fi
if ! grep -q 'if a.isNotFC' "$tmp/packages/envd/internal/api/init.go"; then
  echo "patched source lost Firecracker access-token validation gate" >&2
  exit 1
fi

echo "source checksums and audited patch application: OK"
