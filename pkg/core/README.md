# pkg/core — flow runner

A small, typed engine that executes a **flow** (a DAG of nodes) — HTTP requests,
assertions, branches, loops, polling, SSE, sub-flows — and returns a structured
per-node result.

It is a greenfield rewrite of the old `pkg/engine`/`pkg/node` runner, built for
three goals:

1. **Simple to extend** — adding a node, an assertion operator, or an output is a
   few lines in one file. No engine changes.
2. **Typed** — the only place dynamic (`any`) data lives is `value.Value`. Every
   other package is statically typed and touches runtime data through it.
3. **No god-context** — a node receives an explicit, narrow `Runtime` of the
   effects it may use (HTTP, Clock, Subflow, Vars). Nothing else.

---

## Quick start

```go
import (
    "context"

    "github.com/nanostack-dev/echopoint-runner/pkg/core/engine"
    "github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
    "github.com/nanostack-dev/echopoint-runner/pkg/core/nodes"  // registers the built-in node kinds
    "github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

f, err := flow.Parse([]byte(`{
  "name": "demo",
  "nodes": [
    {"id": "login", "type": "request", "method": "POST", "url": "https://api/login",
     "outputs": [{"name": "token", "path": "body.access_token"}]},
    {"id": "me", "type": "request", "url": "https://api/me",
     "headers": {"Authorization": "Bearer {{login.token}}"},
     "assertions": [{"path": "status", "op": "equals", "expected": 200}]}
  ],
  "edges": [{"source": "login", "target": "me"}]
}`))
if err != nil { /* ... */ }

eng := engine.New(nodes.DefaultRuntime(), nil) // resolve=nil: no sub-flows
res := eng.RunFlow(context.Background(), f, value.Map{})

fmt.Println(res.Success)              // true/false for the whole run
fmt.Println(res.Nodes["me"].Status)   // success | failed | skipped
```

> **Registration is import-triggered.** The built-in kinds register themselves in
> `init()`. Blank-import `pkg/core/nodes` (as above, or `import _ ".../pkg/core/nodes"`)
> so the registry is populated before you parse a flow.

---

## Mental model — three things

```
value.Value   the ONLY place `any` lives. a box of decoded JSON + typed getters.
the node seam  a node = a typed Cfg (embeds node.Base) + one Run function.
the engine     orchestration ONLY: schedule, assert/output, recurse. no per-kind logic.
```

Every other package serves one of these.

---

## Package layout

| Package        | Role                                                                 |
|----------------|----------------------------------------------------------------------|
| `value`        | `value.Value` — the dynamic-data boundary. JSONPath `Get`, typed getters. |
| `assert`       | Declared assertions: a `Spec` (path, op, expected) evaluated over a value. |
| `output`       | Declared outputs: bind a name to a JSONPath into a value.            |
| `dynamicvars`  | `{{$gen:args}}` dynamic-variable generators.                        |
| `flow`         | The parsed graph: pure data. Imports neither `node` nor `engine`.    |
| `tmpl`         | Resolves `{{ref}}` / `{{{ref}}}` / `{{$dyn}}` in a node's raw JSON.  |
| `node`         | **The authoring seam**: `Base`, `Runtime`, `Result`, `Register`, capabilities. |
| `nodes`        | The nine built-in node kinds (one file each) + `DefaultRuntime`.     |
| `engine`       | The scheduler + assert/output post-step + sub-flow recursion + middleware/observer. |
| `result`       | The outcome types: `FlowResult`, `NodeResult`, statuses, skip reasons. |

Dependencies point strictly upward: `value` ← `assert`/`output`/`tmpl` ← `node` ←
`nodes` / `engine`. No cycles.

---

## Execution lifecycle

What `RunFlow` does, in order:

```
JSON ─Parse→ Flow ─validate→ schedule ─loop→ [ classify → step → runNode ] → FlowResult
                                                                  │
                          template → decode → Run → assert/output → middleware
                                               │
                          (composite Run → recurse into the engine)
```

1. **Parse** (`flow.Parse`) — text → a graph of `Node{ID, Kind, Raw}` + `Edge`s.
   Each node's full object is kept as opaque `Raw` bytes; `flow` never learns what
   a kind means.
2. **Validate** (`engine.validateFlow`) — edges reference real nodes; every
   **Router** node's targets have an edge; every **FlowReferencer**'s child flow
   exists and the reference graph is acyclic. All generic — no per-kind knowledge.
3. **Schedule** (`engine.schedule`) — Kahn topological order. Roots (indegree 0)
   seed a ready queue; finishing a node releases its successors. An output `store`
   (`map[nodeID]→outputs`, with `""` holding flow inputs) grows as nodes complete.
4. **classify** (`engine.classify`) — per node, decide run vs skip from predecessor
   state: a live succeeded edge → run; a failed/skipped/routed-away predecessor →
   skip with a reason; `run_when: always` nodes run for cleanup even after a
   failure.
5. **step** — run or skip, record a `NodeResult`, emit an event, release
   successors. **A failure is recorded, not thrown** — dependents skip, the rest of
   the flow continues.
6. **runNode** — the universal per-node pipeline:
   1. **template** the raw JSON against the input view (`tmpl.Resolve`),
   2. **decode** the resolved bytes into typed config (`node.Decode`),
   3. **run** the node (`Bound.Run`),
   4. for a *provider* node, **assert + extract outputs** uniformly.
   Steps 3–4 are wrapped as one unit by any **middleware** (retry re-asserts too).
7. **recurse** — `module`/`loop`/`poll` call back into the engine through the
   injected `Subflow` dependency; the engine is its own `SubflowRunner`.

---

## Node catalog

All nine kinds. `type` is the JSON discriminator.

| `type`         | Purpose                                                    | Key fields | Outputs |
|----------------|------------------------------------------------------------|------------|---------|
| `request`      | HTTP request; exposes the response for assertions/outputs  | `method`, `url`, `headers`, `body`, `timeout_ms` | `{status, headers, body}` |
| `set_variable` | Compute named values from templates                        | `variables` | the computed map |
| `assert`       | Validate already-produced data by fully-qualified path     | `assertions` (on `Base`) | — |
| `branch`       | Route to one successor by the first matching case          | `cases[]{when, target}`, `default` | `{matched, matched_index}` |
| `loop`         | Foreach an inline body flow; aggregate iteration outputs   | `items`, `body`, `item_var`, `index_var`, `max_iterations`, `continue_on_error` | `{results, count}` |
| `poll`         | Re-run a body until the exit assertions pass (or budget)   | `body`, `assertions`, `max_attempts`, `interval_ms`, `timeout_ms` | `{attempts, result}` |
| `sse`          | Consume `text/event-stream`; assert per event until a stop | `url`, `headers`, `max_events`, `completion_event`, `stop_on_assertion_failure`, `timeout_ms` | `{events, count, last, stop_reason}` |
| `module`       | Run a named child flow once with inputs                    | `body_flow_id`, `inputs` | the child's outputs |
| `delay`        | Sleep (cancellation-aware)                                 | `duration_ms` | `{delayed_ms}` |

Every node also accepts the common `Base` fields: `id`, `display_name`,
`run_when` (`on_success` default, or `always`), `assertions`, `outputs`.

### Result modes

A node's `Run` returns a `node.Result` shaped one of four ways — this is how the
engine knows what post-processing to apply:

- **provider** (`request`, `set_variable`, `loop`, `assert`): sets `Provided:true`
  and an `Assert` value → the framework runs the node's declared assertions/outputs.
- **self-evaluating** (`poll`, `sse`): evaluates its own assertions internally and
  returns them on `Assertions` → the framework records but does not re-run them.
- **routing** (`branch`): returns `Routed` (the chosen successors) → the engine
  marks the not-taken edges dead.
- **plain** (`delay`, `module`): just `Outputs`.

---

## Assertions, outputs, templating

All three address data with the **same** path syntax (`value.Value.Get`):

- A bare dotted path — `body.access_token`, `headers.content-type`,
  `create-user.id` (hyphens fine) — resolves member-by-member.
- A path starting with `$` is full RFC-9535 JSONPath — `$.items[*].id`,
  `$.data[?@.active]`.

**Assertion operators** (`assert.Op`):

| Group    | Operators |
|----------|-----------|
| Equality | `equals`, `not_equals` |
| String   | `contains`, `not_contains`, `starts_with`, `ends_with`, `regex` |
| Presence | `empty`, `not_empty`, `exists` |
| Numeric  | `gt`, `lt`, `gte`, `lte`, `between` (`expected: [min, max]`) |

Equality/substring operators are **deliberately lenient** — they compare
stringified forms, so `200 == "200"`. Numeric operators compare real numbers
(fractional values are not truncated).

**Templating** (`tmpl`), resolved *before* a node is decoded, so a node never sees
a template:

| Form          | Meaning |
|---------------|---------|
| `{{ref}}`     | inline string interpolation |
| `{{{ref}}}`   | whole-value substitution (preserves object/number/bool type) |
| `{{$name:a:b}}` | a dynamic-variable generator (see `dynamicvars`) |

`ref` is the same path syntax: `login.token`, `$.items[0].id`, or a bare flow-input
name. **Unresolved refs are left verbatim** so a typo is visible, not silently empty.

---

## Result model

`RunFlow` returns a `*result.FlowResult` — node failures are recorded here, never
returned as a Go error.

```go
type FlowResult struct {
    Success bool                    // false if any on_success node failed, or validation failed
    Nodes   map[string]*NodeResult  // per-node outcome
    Outputs value.Map               // every node's outputs, nested under its id
    Error   string                  // first main-phase failure (or validation/cancel)
    Code    string
}

type NodeResult struct {
    ID, Kind   string
    Status     Status          // success | failed | skipped
    Outputs    value.Map
    Assertions assert.Results   // per-assertion outcomes
    Error, Code, SkipReason string
}
```

**Skip reasons** (a wire contract — exact strings must not drift):
`dependency_failed`, `dependency_skipped`, `routed_away_by_branch`,
`aborted_after_failure`, `missing_inputs`.

**Error codes** — a stable taxonomy. User-caused failures carry a code; a genuine
runner fault is `RUNNER_ERROR`.

| Code | When |
|------|------|
| `FLOW_VALIDATION_FAILED` | empty flow, bad edge, cycle, unreachable nodes |
| `INVALID_NODE_CONFIG` | template or decode error on a node |
| `ASSERTION_FAILED` | a declared assertion failed |
| `UNKNOWN_REFERENCE` | assert/branch referenced an unexecuted/unknown node |
| `REQUEST_FAILED` | HTTP build/transport/read error |
| `LOOP_FAILED` | items not a list, body parse error, or an iteration failed |
| `POLL_FAILED` / `POLL_BODY_FAILED` / `POLL_TIMEOUT` / `POLL_CONDITION_NOT_MET` | poll setup / body / deadline / budget exhausted |
| `SSE_FAILED` | connect, non-2xx, read, or assertion failure on the stream |
| `MODULE_FLOW_NOT_FOUND` / `MODULE_CYCLE_DETECTED` / `MODULE_FAILED` | sub-flow resolution / recursion / child failure |
| `SUBFLOW_FAILED` | inline body (loop/poll) failed |
| `CANCELLED` | the context was cancelled mid-run |
| `RUNNER_ERROR` | an unexpected non-user fault |

On a sub-flow failure the child's real code and message propagate up, so a
loop/poll/module surfaces *which inner node* failed, not a generic wrapper.

---

## Extending

### Add a node kind

One file. No engine change. Write a config struct that embeds `node.Base`, a `Run`
function, and register it:

```go
package nodes

type EchoCfg struct {
    node.Base
    Message string `json:"message"`
}

func runEcho(_ context.Context, cfg EchoCfg, _ value.Value, _ node.Runtime) (node.Result, error) {
    return node.Result{Outputs: value.Map{"echo": value.Of(cfg.Message)}}, nil
}

func init() { node.Register(spi.KindEcho, runEcho) } // add KindEcho to pkg/spi/kind.go
```

- Embedding `node.Base` is what lets you register — the seam is sealed (only
  `Base`-embedders satisfy the constraint).
- Need effects? Take them off `rt node.Runtime` (`rt.HTTP`, `rt.Clock`,
  `rt.Subflow`, `rt.Vars`). Need nothing? Ignore it.
- Want the framework to run assertions/outputs against your result? Set
  `Provided: true` and an `Assert` value.
- Want validation to see references or route targets? Implement
  `node.FlowReferencer` or `node.Router` — the engine picks them up generically.

### Add an assertion operator

Add the constant and a `case` in `assert.compare` (`assert/assert.go`). Nothing
else.

### Add an output

Outputs are declarative already — `{"name": ..., "path": ...}` on any provider
node. No code needed.

---

## Runtime, middleware, observer

```go
// The explicit effect set. Build one with only the fields your flow needs.
type Runtime struct {
    HTTP    HTTPDoer          // request/sse
    Clock   Clock             // delay/poll (WallClock in prod; fakeable in tests)
    Subflow SubflowRunner     // injected by engine.New — do not set
    Vars    DynamicResolver   // {{$dyn}} generators
}

eng := engine.New(nodes.DefaultRuntime(), resolveChildFlow,
    engine.WithMiddleware(engine.Retry(3), engine.Timeout(10*time.Second)),
    engine.WithObserver(func(ev engine.Event) { /* progress streaming */ }),
)
```

- **Middleware** wraps each node's *run-and-assert* unit, so `Retry` re-runs the
  assertions too; `Retry` waits a small ctx-respecting backoff between attempts.
- **Observer** receives `NodeStarted`/`NodeCompleted`/`NodeFailed` and the
  flow-level events, for the **top-level flow only** — a node running a sub-flow
  emits as a single node event; its inner nodes are silent (keeps the wire flat).

---

## Testing

```
gofmt -w pkg/core/
go test -race -count=1 ./pkg/core/...
golangci-lint run --path-mode=abs --timeout 5m ./pkg/core/...
```

Each leaf package has focused unit tests; `engine/engine_test.go` drives whole
flows against `httptest` servers with a `fakeClock`, covering every node kind, the
skip cascade, routing, sub-flow recursion, and the middleware.

---

## Benchmarks

`engine/bench_test.go` stresses the engine with generated graphs. To keep the
measurement on *engine* overhead rather than the network, HTTP/SSE are served by
an instant in-memory `node.HTTPDoer` (canned responses, zero latency) and time by
a `fakeClock`. A separate `BenchmarkRealisticHTTP` drives a real `httptest` server
(the "wiremock" end-to-end sanity check).

```
go test ./pkg/core/engine/ -run '^$' -bench . -benchmem
```

Graph shapes: `WideDiamond` (root → N parallel → sink asserting over all N),
`DeepChain` (N templated requests in series), `Loop` (foreach over N items),
`SSE` (N-event stream).

Two optimizations came out of profiling these:

1. **Incremental input view.** `runNode` used to rebuild the whole output store
   into a fresh map on every node (`inputView`), re-boxing each node's outputs
   each time — O(n²) allocation, ~74% of all allocs on a wide graph. The
   scheduler now maintains the denormalized view incrementally (publish each
   node's outputs once on completion; box in O(1) per step). WideDiamond n=256:
   **−79% memory, −67% time, −52% allocs**; scaling went from quadratic to linear.
2. **Capability-indexed validation.** `validateTargets`/`walkRefs` decoded *every*
   node just to check whether it routes or references a flow. Each kind's
   capabilities are now probed once at `Register` (`node.Routes`/`node.References`),
   so validation skips decoding kinds that can never route/reference — ~8% fewer
   allocs across the board.

## Design principles (the invariants worth keeping)

- **`any` lives only in `value`.** Every other package is statically typed.
- **The engine has no per-node-type logic.** Every kind dispatches through the same
  registry; branch/module specifics are surfaced via capability interfaces.
- **Nodes are tiny** (30–90 lines). Scheduling, templating, asserting, skipping and
  recursing live once in the engine; a node just returns a `Result`.
- **Explicit dependencies, no god-context.** `context.Context` is for cancellation;
  effects come from `Runtime`.
- **Failure-continue.** One node failing records an outcome and skips dependents;
  the caller always sees the whole run.
