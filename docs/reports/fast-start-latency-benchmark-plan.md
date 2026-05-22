# Fast-Start Latency Benchmark Plan

## Goal

Reproduce the startup-latency path described in `README_zh.md`:

- single-concurrency startup around `60ms`
- `50` concurrent creations around `67ms` average, `90ms` P95, `137ms` P99

This plan targets the real fast path in this repository: create a sandbox from an already-built, already-distributed, node-local `v2` template snapshot.

## What The `60ms` Number Actually Means

The `60ms` claim is not a full "build a template, boot a fresh OS, and become ready" number.

It is the latency of:

1. sending `POST /sandboxes` with a `templateID`
2. resolving that request to `cube.master.appsnapshot.template.id + version=v2`
3. scheduling onto a healthy node that already has the template locally
4. restoring a new MicroVM from the local snapshot
5. returning the create response

The relevant repository paths are:

- `CubeAPI/src/handlers/sandboxes.rs`
- `CubeMaster/pkg/service/httpservice/cube/cubeboxutil.go`
- `CubeMaster/pkg/templatecenter/store.go`
- `CubeMaster/pkg/selector/filter/template_locality.go`
- `Cubelet/plugins/cube/internals/appsnapshot/appsnapshot_plugin.go`
- `Cubelet/pkg/controller/runtemplate/template_manager.go`
- `Cubelet/plugins/cbri/cubeboxcbri/cubebox.go`

## Environment Configuration

### Required

- `x86_64` Linux
- KVM available and usable
- one deployed Cube Sandbox environment with local API reachable
- one template in `READY` state
- that template already distributed to the benchmark node

### Strongly Recommended For Reproducing README Numbers

- run on a physical bare-metal server
- run benchmark client on the same node as CubeAPI and use `http://127.0.0.1:3000`
- keep the machine otherwise idle during the benchmark window
- avoid nested virtualization, WSL, or remote-client measurement if the target is the README number

### Shipped Runtime Settings To Keep First

The default Cubelet config already reflects the intended fast path:

- `Cubelet/config/config.toml`
- cgroup pool: `pool_size = 3000`
- storage pool: `pool_type = "copy_reflink"`, `pool_size = 3000`
- workflow create concurrency: `concurrent = 100`
- network-agent enabled
- template snapshot spec file: `/usr/local/services/cubetoolbox/cube-snapshot/spec.json`

The default dynamic host quota file does not cap creation concurrency:

- `Cubelet/dynamicconf/conf.yaml`
- `host.quota.creation_concurrent_num: 0`

## Template Configuration

Use the repo quickstart path first:

- `docs/guide/quickstart.md`
- `docs/zh/guide/tutorials/template-from-image.md`

The template must:

- reach `READY`
- expose a real HTTP service
- use the intended readiness probe
- stay local on the node under test

Keep the default template resource shape first unless you have a reason to change it:

- default CPU: `2000m`
- default memory: `2000Mi`

Those values matter because snapshot resolution is resource-spec aware.

## Measurement Tools

### Primary Tool: API-Facing Benchmark

Use the Go benchmark client in:

- `CubeAPI/benchmark`

Why this is the primary tool:

- it directly measures `POST /sandboxes`
- it is low overhead
- it exports JSON with `avg`, `p95`, and `p99`
- it matches the user-visible create latency path

### Secondary Tool: Server-Side Breakdown

Use the detailed analyzer in:

- `dev-env/measure_startup_detailed.py`

Why this is the secondary tool:

- it correlates client-visible create time with CubeMaster and Cubelet log timing
- it helps explain misses against the target
- it surfaces whether the bottleneck is probe, VM restore, network, or another create stage

## Benchmark Procedure

1. Verify the API is reachable on the benchmark node.
2. Verify `CUBE_TEMPLATE_ID` points to a `READY` template.
3. Run a single-concurrency create-only benchmark.
4. Run a `50`-concurrency create-only benchmark.
5. Save all JSON outputs and console logs.
6. If available, run the detailed server-side analyzer for a smaller sample.
7. Compare measured numbers with the README targets.

## Acceptance Criteria

### Single-Concurrency Target

- expected path: create-only, `c=1`
- target reference: around `60ms` average

### Fifty-Concurrency Target

- expected path: create-only, `c=50`
- target reference: `67ms` average, `90ms` P95, `137ms` P99

## Common Reasons For Missing The Target

- the node is virtualized instead of bare metal
- the template is not local on the scheduled node
- the template is not fully `READY`
- the benchmark client is remote and adds network latency
- the template image, probe behavior, CPU, or memory differ from the intended baseline
- the host is under background CPU, I/O, or memory pressure

## Runnable Wrapper

The wrapper added for this plan is:

- `dev-env/run_fast_start_benchmark.sh`

It performs:

- environment validation
- `cube-bench` build
- single-concurrency benchmark run
- `50`-concurrency benchmark run
- JSON artifact export
- optional detailed analyzer execution
- target comparison summary
