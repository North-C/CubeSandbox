# ARM64 performance image

This directory reproduces the best validated OCI image used by the 2U2G
Template performance tests. It builds both optimized components from source;
no locally compiled binaries are committed.

```text
Cubelet readiness probe
          |
          v
native code server v3 :49999 ---- POST /execute ----> fresh Python process
          |
          | readiness cached after first success
          v
patched envd :49983 -------- MMDS prime during Template capture
                                    |
                                    +--> stop after restored clock deadline
```

## Pinned inputs

| Input | Pinned version |
|---|---|
| Base image | `cubesandbox-bench/sandbox-code-envd-ci@sha256:21255c98dfb3863d153f2b83122a90b04d099d545fad16add5841dacb4fcdbbb` |
| envd source | `e2b-dev/infra@b8ca332f435370397bf42be614b2a5b620d65d39` (envd 0.5.13) |
| Go builder | `golang:1.26.2-bookworm` |
| build target | `linux/arm64` |

## Build

Run from this directory with Docker BuildKit/buildx:

```bash
docker buildx build \
  --platform linux/arm64 \
  --load \
  -t cubesandbox-bench/sandbox-code-envd-ci:arm64-optimal .
```

The final stage replaces the base image's `/usr/bin/envd`, native code server,
and existing `/usr/local/bin/start-envd-code-interpreter.sh` entrypoint target.

## Template contract

The validated Template uses 2 vCPU, 2 GiB memory, a 1 GiB writable layer,
ports 49983 and 49999, and an HTTP readiness probe on port 49999. The native
server does not bind 49999 until envd is healthy, so a successful probe means
both services required by the SDK are ready.

The separate cross-stage early-probe experiment is intentionally disabled.
Do not install the experimental Cubelet systemd drop-in when reproducing this
image's recorded results.
