# Autoresearch: parallel runner efficiency

## Objective
Improve how `echopoint-runner` executes request-heavy flows that contain independent branches.
The current runtime already runs multiple claimed jobs concurrently, but the flow engine walks ready nodes one at a time. This session focuses on the hottest part of runner work: parse flow JSON, build the engine, execute a fan-out/fan-in flow against WireMock, and measure end-to-end cost per flow execution.

## Metrics
- **Primary**: `parallel_flow_ns_per_op` (`ns/op`, lower is better)
- **Secondary**: `parallel_flow_b_per_op`, `parallel_flow_allocs_per_op`

## How to Run
`./autoresearch.sh` - starts or reuses a local WireMock container, runs a targeted Go benchmark, and prints `METRIC name=number` lines.

## Files in Scope
- `pkg/engine/execution.go` - flow scheduling and node execution hot path.
- `pkg/engine/engine.go` - engine construction helpers and traversal metadata.
- `internal/runtime/runtime.go` - runner job orchestration if engine changes expose runtime wins.
- `internal/runtime/event_reporter.go` - event flushing overhead if reporter becomes part of the bottleneck.
- `pkg/engine/parallel_benchmark_test.go` - benchmark workload for the autoresearch loop.
- `it/wiremock/stubs/bench-*.json` - delayed HTTP fixtures used by the benchmark.
- `autoresearch.sh` - benchmark runner and metric parser.
- `autoresearch.jsonl` - source of truth for experiment history.
- `autoresearch-dashboard.md` - current segment dashboard.
- `experiments/worklog.md` - narrative log and idea backlog.

## Off Limits
- Public flow JSON contract in `pkg/flow` unless required for correctness.
- Node type APIs in `pkg/node` unless a change is clearly required for the optimization.
- Dependency set in `go.mod`.
- Production config defaults in `internal/config/config.go` unless directly tied to a measured gain.

## Constraints
- Preserve existing flow semantics and observer events.
- Keep existing tests passing.
- No new dependencies.
- Benchmark must stay runnable on a local machine with Docker and should remain fast enough for repeated experiments.
- Prefer simpler scheduling changes over invasive architectural rewrites.

## What's Been Tried
- Run 1 (`3705141`, keep): established a clean baseline after disabling benchmark logging noise. Baseline is `796991230 ns/op`, `301328 B/op`, `2442 allocs/op`.
- Run 2 (`fc435aa`, keep): executed all currently ready nodes concurrently inside `pkg/engine/execution.go`. This dropped the primary metric to `167201083 ns/op` (-79.0%), confirming that within-flow serialization was the dominant cost on this workload.
- Run 3 (`14c48f9`, keep): wrapped observers with a synchronized adapter so existing callbacks remain safe under parallel node execution. Metric improved again to `129007182 ns/op` (-83.8%).
- Run 4 (`14c48f9`, discard): precompiled template regexes in `pkg/node`, which reduced allocations but regressed the primary metric to `222101774 ns/op`. Allocation-only wins are not enough here.
- Run 5 (`14c48f9`, discard): reused a shared `http.Client` in request nodes. It came close (`131964369 ns/op`) but still lost to the current best.
- Run 6 (`948b0aa`, keep): added singleton-batch fast paths in the scheduler to skip sorting and goroutine fan-out when only one node is ready. Metric improved to `57957945 ns/op` (-92.7%).
- Run 7 (`2fd3963`, keep): iterated ready nodes in declared flow order instead of sorting by ID every loop. Metric improved again to `57640743 ns/op` (-92.8%).
- Run 8 (`HEADPEND`, keep): introduced a read-only `OutputView` snapshot for `ExecutionContext.AllOutputs` and copied committed node outputs before storing them. Metric improved to `56074748 ns/op` (-93.0%) while closing the shared-state mutation risk in the parallel engine.
- Key observation from source review: `pkg/engine/execution.go` currently executes exactly one ready node per loop iteration, so "parallel" flow shapes are still serialized inside a single flow execution.
- Existing runtime concurrency (`internal/runtime/runtime.go`) already allows multiple claimed jobs via `MaxParallelFlows`, so engine-level fan-out looks like the clearest first lever.
- New architectural insight: the scheduler can parallelize ready-node batches safely as long as dependency mutation and output publication happen after the batch joins, and observer callbacks are serialized at the boundary.
- New negative result: request-path micro-optimizations that mainly reduce allocations can still lose on latency; benchmark decisions must continue to follow `ns/op` first.
- Another negative result: per-node `http.Client` construction is not the dominant remaining cost in this benchmark.
- New architectural insight: once wide fan-out work runs concurrently, the scheduler should specialize small ready sets instead of treating them like full parallel batches.
- Another architectural insight: the flow definition already provides a deterministic node order, which is cheaper than recomputing a sorted ready set each iteration.
- Long-term correctness insight: `AllOutputs` should be treated as immutable historical state, not a live shared map. A read-only snapshot API makes the parallel scheduler safe by construction.
