# Worklog: parallel runner efficiency

## Session
- Goal: improve `echopoint-runner` efficiency for request-heavy flows with independent branches.
- Benchmark command: `./autoresearch.sh`
- Primary metric: `parallel_flow_ns_per_op` (`ns/op`, lower is better)
- Secondary metrics: `parallel_flow_b_per_op`, `parallel_flow_allocs_per_op`
- Workload: parse + build + execute a WireMock-backed fan-out/fan-in flow in `pkg/engine/parallel_benchmark_test.go`.

## Runs
### Run 1: baseline - parallel_flow_ns_per_op=796991230 (KEEP)
- Timestamp: 2026-03-25 15:28
- What changed: finalized the benchmark harness by disabling zerolog output during the benchmark so the measurements reflect engine and request work instead of console I/O.
- Result: `parallel_flow_ns_per_op=796991230`, `parallel_flow_b_per_op=301328`, `parallel_flow_allocs_per_op=2442`, delta vs best `+0.0%`.
- Insight: the benchmark now shows the true baseline for a seven-node fan-out/fan-in HTTP flow; source review still indicates the engine serializes all six independent branch requests.
- Next: batch and parallelize execution of all currently ready nodes while preserving dependency semantics and observer callbacks.

### Run 2: execute independent ready nodes concurrently - parallel_flow_ns_per_op=167201083 (KEEP)
- Timestamp: 2026-03-25 15:29
- What changed: changed `pkg/engine/execution.go` to collect all currently ready nodes, execute them in goroutines, then merge outputs and dependency updates after the batch completes.
- Result: `parallel_flow_ns_per_op=167201083`, `parallel_flow_b_per_op=352666`, `parallel_flow_allocs_per_op=2658`, delta vs baseline `-79.0%`.
- Insight: the main bottleneck really was serialized branch execution; even with a small allocation increase, overlapping the six delayed HTTP branches produced the first major win.
- Next: harden callback semantics and thread-safety around observers before chasing smaller scheduler optimizations.

### Run 3: serialize observer callbacks for parallel execution - parallel_flow_ns_per_op=129007182 (KEEP)
- Timestamp: 2026-03-25 15:30
- What changed: wrapped custom observers with a mutex-backed adapter in `pkg/engine/observer.go` so parallel node execution keeps callbacks safe for existing non-thread-safe observers and runtime reporters.
- Result: `parallel_flow_ns_per_op=129007182`, `parallel_flow_b_per_op=341901`, `parallel_flow_allocs_per_op=2654`, delta vs baseline `-83.8%`, delta vs best `+0.0%`.
- Insight: observer serialization preserved correctness without giving back the scheduler win; performance even improved a bit more, likely from reduced contention and steadier execution ordering.
- Next: trim per-node overhead in request execution or schema inference, especially repeated input/output schema computation and template-regex work.

### Run 4: precompile template regexes only - parallel_flow_ns_per_op=222101774 (DISCARD)
- Timestamp: 2026-03-25 15:45
- What changed: tried moving regex compilation out of the hot path in `pkg/node/schema_inference.go` and `pkg/node/template_resolver.go` without changing the overall request execution structure.
- Result: `parallel_flow_ns_per_op=222101774`, `parallel_flow_b_per_op=228227`, `parallel_flow_allocs_per_op=1458`, delta vs best `+72.2%`.
- Insight: allocation counts dropped sharply, but wall-clock time regressed enough that this alone is not the right next lever for the primary metric.
- Next: focus on scheduler/request-path latency improvements first; come back to allocation-only wins later if they can be paired with a time win.

### Run 5: reuse a shared http client in request nodes - parallel_flow_ns_per_op=131964369 (DISCARD)
- Timestamp: 2026-03-25 15:51
- What changed: tried replacing per-request `http.Client` construction with a shared package-level client in `pkg/node/request_node.go`.
- Result: `parallel_flow_ns_per_op=131964369`, `parallel_flow_b_per_op=399348`, `parallel_flow_allocs_per_op=2665`, delta vs best `+2.3%`.
- Insight: the idea was close, but still slower than the current best and increased allocations, so connection setup is not the main remaining limiter under this benchmark.
- Next: look for scheduler overhead we can avoid on singleton batches or reduce work done between the parallel branch batch and the fan-in node.

### Run 6: fast-path singleton ready batches - parallel_flow_ns_per_op=57957945 (KEEP)
- Timestamp: 2026-03-25 15:53
- What changed: added scheduler fast paths in `pkg/engine/execution.go` to avoid sorting and goroutine fan-out when only one node is ready, which is exactly the common fan-in stage after the parallel branch batch.
- Result: `parallel_flow_ns_per_op=57957945`, `parallel_flow_b_per_op=385893`, `parallel_flow_allocs_per_op=2652`, delta vs baseline `-92.7%`, delta vs best `+0.0%`.
- Insight: after the major branch-parallelization win, the remaining hot scheduler cost was mostly overhead around tiny ready sets; removing that overhead unlocked another large latency reduction.
- Next: consider whether the first ready batch can also avoid full sorting, or whether parse/build work should be separated from execution if we want the runner metric to move even further.

### Run 7: preserve flow order in ready node scans - parallel_flow_ns_per_op=57640743 (KEEP)
- Timestamp: 2026-03-25 15:54
- What changed: stopped sorting ready nodes by ID and instead scanned `engine.flow.Nodes` in declared flow order, which preserves deterministic scheduling while avoiding per-iteration sort work.
- Result: `parallel_flow_ns_per_op=57640743`, `parallel_flow_b_per_op=327939`, `parallel_flow_allocs_per_op=2634`, delta vs baseline `-92.8%`, delta vs best `+0.0%`.
- Insight: even after singleton fast paths, a little scheduler bookkeeping still mattered; using existing flow order is simpler and slightly faster than rebuilding sorted order each round.
- Next: benchmark execution separately from parse/build, or explore reusing dependency bookkeeping structures across executions.

### Run 8: make AllOutputs a read-only snapshot view - parallel_flow_ns_per_op=56074748 (KEEP)
- Timestamp: 2026-03-25 16:11
- What changed: replaced mutable `ExecutionContext.AllOutputs` maps with a read-only `OutputView` snapshot API in `pkg/node`, threaded that snapshot through validation/input assembly in `pkg/engine`, and defensively copied committed node outputs before storing them for later batches.
- Result: `parallel_flow_ns_per_op=56074748`, `parallel_flow_b_per_op=365472`, `parallel_flow_allocs_per_op=2693`, delta vs baseline `-93.0%`, delta vs best `+0.0%`.
- Insight: the long-term correctness fix also slightly improved the primary metric while making the parallel scheduler deterministic and safe against accidental shared-state mutation.
- Next: if more performance work is needed, measure execution-only paths separately from parse/build and consider reusing `executionState` structures.

## Key Insights
- The current engine scheduler in `pkg/engine/execution.go` drains only one ready node at a time.
- The runtime can already keep multiple jobs in flight; the more obvious gap is within-flow fan-out execution.
- Benchmark logging noise was material; silencing it cut baseline cost enough to make later comparisons meaningful.
- Batching all zero-dependency nodes in one scheduler step is a high-leverage change for request-heavy flows with independent branches.
- Existing observers were not guaranteed to be thread-safe; parallel execution needs a synchronized observer boundary.
- Lower allocations do not automatically improve the primary metric on this workload; the dominant cost remains request-path latency and scheduling overhead.
- Reusing one shared `http.Client` does not beat the current best, so the current benchmark is likely dominated more by branch orchestration and request issuance timing than client allocation cost.
- Ready-set overhead matters: once parallel execution is in place, the scheduler's small-batch bookkeeping becomes a meaningful second-order cost.
- Existing flow declaration order is a good deterministic iteration order; there is no need to sort ready sets separately.
- The engine now has a cleaner contract: nodes may read committed historical outputs through `OutputView`, but cannot mutate scheduler state or see sibling in-flight updates.

## Next Ideas
- Cache request node input/output schema results instead of recomputing template extraction on every execution.
- Precompile template regular expressions or reduce repeated resolver setup in `pkg/node`.
- If engine-local gains plateau, profile runtime overhead from parse/build and event reporting.
- Skip goroutine setup for singleton ready batches such as the fan-in join node.
- Cache sorted ready-node ordering or avoid sorting when there is only one ready node.
- Separate parse/build from execute in a second benchmark so future experiments can distinguish engine construction cost from execution cost.
- Explore pooling or reusing `executionState` maps when a single `FlowEngine` executes many jobs.
- Consider a dedicated unit test for `OutputView` semantics and batch isolation guarantees.
