# Snapshot-only MMDS priming

This patch targets envd commit
`b8ca332f435370397bf42be614b2a5b620d65d39` (envd 0.5.13).

The original envd MMDS retry loop keeps virtio-net activity present while
CubeSandbox captures the VM. That activity was required for reliable
openEuler guest reset under concurrent restore on the tested ARM64 host.

`-prime-mmds-until-unix` retains the original loop during Template capture.
After restore, `ResetVm` advances the guest wall clock beyond the saved
deadline and envd cancels MMDS polling on the next 50 ms tick. The envd API
also suppresses later `PostInit` polling in this mode.

The retained 50 ms variant completed three c50/n500 runs with 500/500
successes and average latencies of 199.59, 202.92, and 205.64 ms before the
native code server optimization was added. Faster experimental variants were
not stable and are intentionally excluded.
