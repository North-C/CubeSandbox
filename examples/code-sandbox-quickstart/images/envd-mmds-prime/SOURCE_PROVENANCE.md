# envd source provenance

- Upstream: <https://github.com/e2b-dev/infra.git>
- Commit: `b8ca332f435370397bf42be614b2a5b620d65d39`
- Upstream tag at that commit: `2026.16`
- envd version: `0.5.13` (`packages/envd/pkg/version.go`)
- Declared Go version: `1.25.4` (`packages/envd/go.mod`)
- License: Apache License 2.0; the upstream root license is retained verbatim as
  `source/LICENSE`.
- Upstream envd Git tree: `a59fdba6bb40b1ef4e3eabae408dbd5fc6dcce04`
- Upstream shared Git tree: `c2d28e78c449de41e47285c714581b71d1ac4753`
- Deterministic `git archive` SHA-256 for the retained upstream paths:
  `14f64eb96729f052ce6eefa81a9b9a4763f81c3e4657671d572cef18f83848e0`.

The archive hash is reproducible from an upstream checkout with:

```sh
git archive --format=tar b8ca332f435370397bf42be614b2a5b620d65d39 \
  LICENSE packages/envd packages/shared/go.mod packages/shared/go.sum \
  packages/shared/pkg/filesystem packages/shared/pkg/id \
  packages/shared/pkg/keys packages/shared/pkg/logger \
  packages/shared/pkg/smap packages/shared/pkg/utils | sha256sum
```

`packages/envd` is the complete 98-file directory from the fixed commit.
envd's fixed `go.mod` contains
`replace github.com/e2b-dev/infra/packages/shared => ../shared`, so the 56 files
under `pkg/filesystem`, `pkg/id`, `pkg/keys`, `pkg/logger`, `pkg/smap`, and
`pkg/utils`, selected by
`GOWORK=off GOPROXY=off go list -deps -test ./...` are retained with
`packages/shared/go.mod` and `go.sum`. Unrelated `shared` services and scripts
are intentionally excluded. No uncommitted file from any E2B or CubeSandbox
checkout is part of this copy.

`SOURCE_SHA256SUMS` records every file under `source/`. Verify it with:

```sh
sha256sum -c SOURCE_SHA256SUMS
```

## Patch lineage

The original MMDS-prime source change was recorded in CubeSandbox commit
`a8ee9c7d3039dc057be1381efb972df5da9f870e`; focused delivery commit
`81de92d8602d4438443cf79225c2a910dbb273cc` has the same stable patch-id
`2e28e75c923b6573e94e9135bd695a9d31f6d0db`. The exact historical patch file
has SHA-256 `552deb861fe3a96c849d74eb8738293939c7963374c4385d111e85d0f4d231d4`.

The delivered patch is intentionally not byte-identical. Re-audit found that
the historical patch passed `isNotFC || prime` into `api.New`; in envd 0.5.13,
`API.isNotFC` also bypasses MMDS access-token hash validation. The delivered
patch adds an independent `disablePostInitMMDSPolling` state and a regression
test, retaining Firecracker authentication while suppressing only the later
PostInit polling loop.

Delivered patch SHA-256:
`b7957a5433715f0dde3d80fea56c7dd3cc0d05e06fc3d45fc5efdf9bc90e2ec3`.
