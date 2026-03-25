# Worklog: parallel runner efficiency

## Session
- Goal: improve `echopoint-runner` efficiency for request-heavy flows with independent branches.
- Benchmark command: `./autoresearch.sh`
- Primary metric: `parallel_flow_ns_per_op` (`ns/op`, lower is better)
- Secondary metrics: `parallel_flow_b_per_op`, `parallel_flow_allocs_per_op`
- Workload: parse + build + execute a WireMock-backed fan-out/fan-in flow in `pkg/engine/parallel_benchmark_test.go`.

## Runs
- Baseline pending.

## Key Insights
- The current engine scheduler in `pkg/engine/execution.go` drains only one ready node at a time.
- The runtime can already keep multiple jobs in flight; the more obvious gap is within-flow fan-out execution.

## Next Ideas
- Execute all currently ready nodes as a batch instead of repeatedly scanning for a single zero-input node.
- Preserve deterministic dependency updates by collecting parallel results before mutating shared state.
- If engine scheduling wins plateau, profile runtime overhead from parse/build and event reporting.
