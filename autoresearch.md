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
- Session initialized. No experiments logged yet.
- Key observation from source review: `pkg/engine/execution.go` currently executes exactly one ready node per loop iteration, so "parallel" flow shapes are still serialized inside a single flow execution.
- Existing runtime concurrency (`internal/runtime/runtime.go`) already allows multiple claimed jobs via `MaxParallelFlows`, so engine-level fan-out looks like the clearest first lever.
