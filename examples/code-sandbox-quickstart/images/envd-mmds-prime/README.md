# Reproducible envd MMDS-prime build

This directory vendors the complete envd 0.5.13 source from
`e2b-dev/infra@b8ca332f435370397bf42be614b2a5b620d65d39`, the exact local
`shared` packages selected by `GOWORK=off go list -deps -test`, the upstream
Apache-2.0 license, per-file SHA-256 checksums, and one auditable downstream
patch. It builds static amd64/arm64 binaries and single-platform
CubeSandbox `sandbox-code` images from digest-pinned inputs.

The patch adds `-prime-mmds-until-unix`. MMDS polling remains active during
the configured snapshot-capture window and is cancelled after the restored
guest clock crosses the absolute deadline. Prime mode separately suppresses
the later `PostInit` polling loop while preserving Firecracker MMDS
access-token validation.

See [README_zh.md](README_zh.md) for the authoritative build, offline bundle,
port/health, smoke-test, snapshot, dependency, and Template instructions.
See [VALIDATION.md](VALIDATION.md) for the recorded 2026-09-18 build and test
results, including the explicit privileged-test limitations.

## Quick start

```sh
make verify
make test
make build-envd
make images

./scripts/inspect-image.sh \
  cubesandbox-dev/sandbox-code:envd-mmds-prime-0.5.13-amd64
./scripts/smoke-image.sh \
  cubesandbox-dev/sandbox-code:envd-mmds-prime-0.5.13-amd64 amd64
```

Generated outputs stay under `_output/`. No script pushes an image, creates a
Template, deploys a host, or changes an existing CubeSandbox resource.

Historical combined variants such as
`arm64-slim-mmds-prime-native-code-v3` are explicitly outside this envd-only
delivery and must not be attributed to envd.
