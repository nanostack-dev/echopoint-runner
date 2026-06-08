# echopoint-runner v2 Public API Redesign

> Status: design proposal. Scope: the public Go API of `echopoint-runner` (`pkg/*` and the transports in `internal/*`). Breaking source changes are acceptable; **behavioral regressions and wire-format breaks are not** — echopoint's SSE dashboards and `openapi.yaml` consume this contract.

This doc combines the winning **Plugin/SPI architecture** with the grafts the judges converged on: ship the **fluent Builder** and **executor middleware** as standalone wins, *finish* (not reinvent) the extractor registry, derive the compatibility map from self-declaration, thread `context.Context` through execution, make `FlowResult` JSON-decodable, and sequence the cross-repo codegen migration. It also corrects two concrete defects the judges caught in the original proposal:

1. **No `json:",inline"`.** `encoding/json` has no such tag. `BaseResult` is an *anonymous embedded field* so it flattens, exactly like today's `BaseExecutionResult` embed.
2. **Generics are demoted.** The engine is interface-first (`AnyResult`); generics appear *only* at typed read sites and the optional `Result[D]` carrier — never forced through the hot JSON-decode path.

---

## 1. Goals & principles

| Principle | What it means here | Anchored against |
|---|---|---|
| **Typed at the edges** | Typed reads (`spi.AsDetail[*request.Detail]`, `GetTyped[T]`), typed `DataRef` instead of re-split strings, typed `Operator`/`Extractor` values. Engine core stays interface-typed (`AnyResult`) so JSON decode never needs a type parameter. | `parseDataRef` re-split on every `assembleInputs`/`validateInputs`/`collectMissingInputs`; `As[]`/`MustAs[]` casts. |
| **Fluent authoring** | A real Go `Builder` (`request.Node("id").POST(...).Assert(...).Extract(...)`). Today the **only** way to make a flow is hand-crafting JSON → `ParseFromJSON`. | `flow.go` is parse-only; no constructors. |
| **Extensible by registration, not edits** | A new node kind / extractor / operator / dynamic var = one self-registering file. Zero edits to `engine`, `flow`, or `registry`. | `UnmarshalNode` switch (`unmarshal.go:17-47`), `UnmarshalExtractor` switch (`factory.go:32-66`), `comparators` map (`assertion.go:149`), `createSkippedNodeResult` switch (`skipped.go:37-44`). |
| **Powerful: cross-cutting concerns have a home** | Retry/backoff/timeout/tracing live in an `Executor` middleware chain. Today there is **zero retry** and the only timeout is `RequestNode`'s hardcoded `defaultRequestTimeoutMs = 30000` built from `context.Background()` — flow-level cancellation cannot even propagate because `Execute` takes no `context.Context`. | `request_node.go:19,297`. |
| **Keep ALL functionality** | Two-phase `on_success`/`always` scheduling, skip-with-reason (all 5 reason codes), cycle/unreachable detection, module cycle detection, `FinalOutputs` flattening, the observer event contract, and all three transports are preserved verbatim. | `execution.go`, `skipped.go`. |
| **Breaking-OK, wire-stable** | Renaming `node.Type → spi.Kind` etc. is fine. `BaseResult`/`FlowResult`/event JSON tags stay byte-identical so echopoint's decoders and `openapi.yaml` are untouched. | `types.go:121-239`, `observer.go`. |

**Non-goals (deliberately scoped out to avoid regression risk):** rewriting the two-phase scheduler into a general event-driven DAG (the skip-frontier semantics in `skipFrontierAlwaysNodes`/`skipBlockedOnSuccessNodes`/`describeSkipCause` are load-bearing for dashboards); turning the observer into a control-flow hook (SSE depends on listener-only semantics — control flow goes through middleware instead).

---

## 2. Architecture

Layered, with a hard SPI seam: **core never imports a concrete kind or capability.** The import graph enforces it.

```
                         ┌──────────────────────────────────────────────┐
   AUTHOR / RUNTIME      │  L5  runner (was internal/runtime,            │
   ───────────────       │      internal/ephemeral, echopoint cloud)     │
                         │      ONE Runner, 3 configs (Observer+Resolver)│
                         └───────────────────────┬──────────────────────┘
                         ┌───────────────────────┴──────────────────────┐
   FLUENT AUTHORING      │  L4  pkg/flow   Parse(data, reg) + Builder    │
                         └───────────────────────┬──────────────────────┘
                         ┌───────────────────────┴──────────────────────┐
   CAPABILITY PLUGINS    │  L3  pkg/kinds/{request,delay,module}         │
   (self-registering)    │      pkg/extractors/* (incl. http)            │
                         │      pkg/operators/*   pkg/dynamicvars        │
                         └──────────┬───────────────────────┬───────────┘
                                    │ register into          │ implement
                         ┌──────────┴────────────┐  ┌────────┴───────────┐
   CORE ENGINE           │  L2 pkg/engine        │  │ L1 pkg/registry    │
   (capability-agnostic) │  scheduler / phases / │  │ Nodes/Extractors/  │
                         │  observers / middleware│  │ Operators/DynVars  │
                         └──────────┬────────────┘  └────────┬───────────┘
                                    └─────────┬──────────────┘
                         ┌────────────────────┴─────────────────────────┐
   THE CONTRACT          │  L0  pkg/spi   Node, Executor, RunContext,    │
   (depends on nothing)  │      AnyResult, OutputView, DataRef, Extractor,│
                         │      Operator, DynamicResolver, ModuleResolver │
                         └────────────────────────────────────────────────┘

   dependency direction (↑ "depends on"):  registry→spi ; engine→spi,registry ;
   kinds/extractors/operators→spi,registry ; flow→spi,registry ; runner→engine,flow
   CORE = {spi, registry, engine}  — never imports request/delay/module/jsonpath/...
```

- **L0 `pkg/spi`** — the contract. `Node`, `Executor` (the one core verb), `RunContext` (interface, was the `ExecutionContext` *struct* — now wrappable by middleware), `AnyResult`, `OutputView`, `DataRef`, plus capability SPIs.
- **L1 `pkg/registry`** — the plugin table that replaces all three switches: `DecodeNode`, `DecodeExtractor`, `Operator(type)`, `DynamicVars`. `registry.Default()` is the standard set; `registry.New()` gives per-tenant/per-test allow-lists (impossible with compile-time switches).
- **L2 `pkg/engine`** — the scheduler, *type-changed but logic-identical*. Two-phase loop, `readyNodes`, `markNodeComplete`, `propagateNodeOutputs`, `skipBlockedOnSuccessNodes`, `skipFrontierAlwaysNodes`, `finalizeExecution` all stay. Adds the `Executor` middleware chain.
- **L3 plugins** — `request`/`delay`/`module` (were `request_node.go` etc.), extractors, operators, dynamic vars. Each self-registers.
- **L4 `pkg/flow`** — `Parse(data, reg)` (registry-driven) + the new `Builder`.
- **L5 `runner`** — one `Runner` type; the three transports become three configurations.

---

## 3. Core types & interfaces

`pkg/spi` — no internal deps. Generics appear only where they earn their keep (typed reads), never in the engine's scheduling path.

```go
package spi

import (
	"context"
	"time"
)

// ---- data references (replaces stringly parseDataRef, re-split 4× today) ----

// DataRef is a parsed "nodeId.outputKey" (Key only, NodeID=="" for flow inputs).
type DataRef struct{ NodeID, Key string }

func ParseDataRef(s string) (DataRef, error) // moved from engine.parseDataRef, now reusable
func (r DataRef) String() string             // "nodeId.key" or "key"

// ---- kinds & phases (wire-identical to node.Type / node.RunWhen) ----

type RunWhen string
const (
	RunWhenOnSuccess RunWhen = "on_success"
	RunWhenAlways    RunWhen = "always"
)

// Kind was node.Type. Now an OPEN string: the registry, not a const switch,
// decides what's valid. "request"/"delay"/"module" are just registered kinds.
type Kind string

// ---- the node, split from its executor so middleware can wrap execution ----

// Node is the engine's view of any node. Replaces node.AnyNode.
// Note: InputSchema returns []DataRef (pre-parsed) instead of []string.
type Node interface {
	ID() string
	DisplayName() string
	Kind() Kind
	RunWhen() RunWhen
	InputSchema() []DataRef
	OutputSchema() []string
	Assertions() []Assertion
	Outputs() []Output
}

// Executor is THE core verb. Was node.AnyNode.Execute. Now takes context.Context
// (graft: today Execute(ctx ExecutionContext) cannot receive cancellation;
// RequestNode builds its own context.Background() deadline at request_node.go:297).
type Executor interface {
	Execute(ctx context.Context, rc RunContext) (AnyResult, error)
}

// Middleware wraps an Executor. The home for retry/timeout/tracing/circuit-break.
type Middleware func(Executor) Executor

// NodeKind is the plugin unit. Registering ONE of these replaces editing
// UnmarshalNode's switch + the createSkippedNodeResult switch + As/MustAs helpers.
type NodeKind interface {
	Kind() Kind
	Decode(raw []byte, reg *Registry) (Node, Executor, error)
	DecodeResult(raw []byte) (AnyResult, error)        // closes the result round-trip hole
	NewSkipped(n Node, base BaseResult) AnyResult       // replaces skipped.go type switch
}

// ---- execution context: interface, not struct, so middleware can decorate ----

type RunContext interface {
	Inputs() map[string]any                                 // assembled "nodeId.key" -> value
	FlowInputs() map[string]any                             // was ExecutionContext.FlowInputs
	Outputs() OutputView                                    // was AllOutputs
	Modules() ModuleResolver
	RunModule(ModuleExecutionRequest) (*FlowResult, error)  // was ModuleExecutor.ExecuteModule
	DynamicVars() DynamicResolver
}

// ---- read view (unchanged in spirit; typed getter added) ----

type OutputView interface {
	HasNode(nodeID string) bool
	Get(nodeID, key string) (any, bool)
	Node(nodeID string) map[string]any
}

// GetTyped narrows at the read site (replaces caller-side any-casts).
func GetTyped[T any](v OutputView, nodeID, key string) (T, bool)
```

### Results — one base shape, typed detail without forcing generics through decode

The judges flagged that the original `json:",inline"` is invalid and that `Result[D]` over the decode path is generics-for-polymorphism. Fix: the **base is anonymously embedded** (flattens exactly like today), the engine handles only `AnyResult`, and the typed `Result[D]` carrier is *optional sugar* for authors.

```go
// AnyResult keeps the EXACT BaseExecutionResult JSON fields echopoint/SSE rely on.
// isResult() seal preserved. Detail() exposes the kind-specific payload opaquely.
type AnyResult interface {
	NodeID() string
	DisplayName() string
	Kind() Kind
	Inputs() map[string]any
	Outputs() map[string]any
	Err() error
	ExecutedAt() time.Time
	SkipReason() *string      // preserves skip-with-reason
	MissingInputs() []string
	Detail() any              // *request.Detail | *delay.Detail | *module.Detail | nil
	isResult()
}

// BaseResult == today's BaseExecutionResult, json tags VERBATIM. Carries the
// AnyResult base accessors + isResult() seal.
type BaseResult struct {
	NodeIDV        string         `json:"node_id"`
	DisplayNameV   string         `json:"display_name"`
	KindV          Kind           `json:"node_type"`        // tag stays "node_type"
	RunWhenV       RunWhen        `json:"run_when,omitempty"`
	InputsV        map[string]any `json:"inputs"`
	OutputsV       map[string]any `json:"outputs"`
	Error          error          `json:"-"`
	ErrorCode      *string        `json:"error_code,omitempty"`
	ErrorMsg       *string        `json:"error_message,omitempty"`
	SkipReasonV    *string        `json:"skip_reason,omitempty"`
	MissingInputsV []string       `json:"missing_inputs,omitempty"`
	ExecutedAtV    time.Time      `json:"executed_at"`
}

// Result[D] is OPTIONAL author sugar. BaseResult is ANONYMOUS (flattens), and the
// kind-specific fields are promoted from D via embedding too — so e.g. a request
// result marshals to {node_id, ..., request_method, response_status_code, ...},
// byte-identical to today's RequestExecutionResult. Engine never names D.
type Result[D any] struct {
	BaseResult        // anonymous embed -> JSON flattens (NOT json:",inline")
	D                 // anonymous embed -> kind fields flatten at top level
}
func (r *Result[D]) Detail() any { return &r.D }

// AsDetail replaces As[T]/MustAs[T] over result structs.
func AsDetail[D any](r AnyResult) (*D, bool) { d, ok := r.Detail().(*D); return d, ok }

// FlowResult == today's FlowExecutionResult (json identical) PLUS a decode path.
type FlowResult struct {
	ExecutionResults map[string]AnyResult `json:"execution_results"`
	FinalOutputs     map[string]any       `json:"final_outputs"` // flattened "nodeId.outputKey"
	Success          bool                 `json:"success"`
	Error            error                `json:"-"`
	ErrorCode        *string              `json:"error_code,omitempty"`
	ErrorMsg         *string              `json:"error_message,omitempty"`
	DurationMS       int64                `json:"duration_ms"`
}
// UnmarshalJSON peeks each entry's "node_type" and dispatches to
// reg.NodeKind(kind).DecodeResult(raw) — closes the round-trip hole where
// ExecutionResults was marshal-only and controlplane double-marshals to map[string]any.
```

### Capability SPIs

```go
type Extractor interface {
	Extract(ResponseContext) (any, error)
	Type() ExtractorType
}
type Operator interface {
	Compare(actual, expected any) (bool, error)
	Type() OperatorType
}
type DynamicResolver interface {                 // unchanged from node.DynamicResolver
	Resolve(name string, args []string) (string, error)
}
type ModuleResolver interface {                  // unchanged from node.ModuleResolver
	ResolveFlow(flowID string) (ResolvedModuleFlow, bool)
}
```

### Observer — unchanged contract, richer event accessors

The observer stays **listener-only** (SSE depends on it). The event structs keep their fields; `NodeFinishedEvent.Result` becomes `spi.AnyResult` (same underlying JSON). `MultiObserver`, `synchronizedObserver`, `ensureSynchronizedObserver`, `NoopObserver` are preserved.

```go
type NodeFinishedEvent struct {
	NodeID, DisplayName string
	NodeType            spi.Kind        // was node.Type
	StartedAt, FinishedAt time.Time
	DurationMs          int64
	Result              spi.AnyResult   // was node.AnyExecutionResult; JSON unchanged
}
```

---

## 4. Fluent authoring example (end-to-end)

`create-customer (POST) → provision-workspace (POST)` with a status-code assertion, an output ref `{{create-customer.customerId}}`, and an always-phase cleanup node. Today this flow can only be expressed as JSON; here it is in code.

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/kinds/delay"
	"github.com/nanostack-dev/echopoint-runner/pkg/kinds/request"
	"github.com/nanostack-dev/echopoint-runner/pkg/registry"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func main() {
	reg := registry.Default() // request+delay+module + all extractors/operators/dynvars

	f := flow.NewBuilder("provision-customer").Version("1").
		Input("baseURL"). // declares an initial input, referenceable as {{baseURL}}

		// on_success POST + status-code assertion + extracted output
		Add(request.Node("create-customer").
			DisplayName("Create customer").
			POST("{{baseURL}}/customers").
			Header("Authorization", "Bearer {{$uuid}}"). // {{$dynamic}} preserved
			JSONBody(map[string]any{"email": "{{$email}}"}).
			Timeout(5*time.Second).
			Assert(request.StatusCode().Equals(201)).         // typed operator dispatch
			Extract("customerId", request.JSONPath("$.id"))). // -> "create-customer.customerId"

		// on_success POST consuming the upstream output ref
		Add(request.Node("provision-workspace").
			DisplayName("Provision workspace").
			POST("{{baseURL}}/workspaces").
			JSONBody(map[string]any{
				"customerId": "{{create-customer.customerId}}", // data-ref propagation
			}).
			Assert(request.StatusCode().Equals(201))).

		// always-phase cleanup (RunWhen=always) — runs even if main phase fails
		Add(delay.Node("settle").
			When(spi.RunWhenAlways).
			For(200*time.Millisecond)).

		Edge("create-customer", "provision-workspace", flow.EdgeSuccess).
		Edge("provision-workspace", "settle", flow.EdgeSuccess).
		Build()

	r := engine.NewRunner(reg,
		engine.WithObserver(engine.LoggingObserver{}),
		engine.WithMiddleware(engine.Retry(3, engine.ExpoBackoff(100*time.Millisecond))),
		engine.WithMiddleware(engine.Timeout(30*time.Second)),
	)

	res, err := r.Run(context.Background(), f, map[string]any{
		"baseURL": "https://api.example.com",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Typed read of a node's detail — replaces MustAs[*RequestExecutionResult]:
	if d, ok := spi.AsDetail[request.Detail](res.ExecutionResults["create-customer"]); ok {
		log.Printf("status=%d assertions=%d", d.ResponseStatusCode, len(d.AssertionResults))
	}
	// FinalOutputs still flattened "nodeId.key":
	log.Printf("customerId=%v", res.FinalOutputs["create-customer.customerId"])
}
```

The JSON on-ramp echopoint actually drives is identical in behavior — `flow.Parse(jsonBytes, reg)` produces the same `*flow.Flow` the Builder does. The Builder is for tests, the CLI, and embedded users.

---

## 5. Extensibility

### (a) Add a custom node kind — one self-registering file

Today this touches ~7-10 sites: `UnmarshalNode` switch, `createSkippedNodeResult` switch, a per-kind result struct + `As/MustAs` helper, `InputSchema`/`OutputSchema` overrides, `GetData`. Now: implement `NodeKind`, call `registry.MustRegisterNode`.

```go
package script

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/registry"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const Kind spi.Kind = "script"

// Detail holds the kind-specific result fields (flatten into the result JSON).
type Detail struct {
	Source     string `json:"source"`
	DurationMs int64  `json:"duration_ms"`
}

type scriptNode struct {
	spi.BaseNode        // shared ID/DisplayName/RunWhen/Assertions/Outputs (was node.BaseNode)
	src string
}

func (n *scriptNode) Kind() spi.Kind            { return Kind }
func (n *scriptNode) InputSchema() []spi.DataRef { return spi.InferTemplateRefs(n.src) } // shared inferencer
func (n *scriptNode) OutputSchema() []string     { return spi.OutputNames(n.Outputs()) }

// Same value is both Node and Executor here.
func (n *scriptNode) Execute(ctx context.Context, rc spi.RunContext) (spi.AnyResult, error) {
	start := time.Now()
	out, err := eval(ctx, n.src, rc.Inputs(), rc.DynamicVars())
	// spi.NewResult kills createSuccessResult/createErrorResult boilerplate (3× today):
	return spi.NewResult(n, rc.Inputs(), out, err, Detail{
		Source:     n.src,
		DurationMs: time.Since(start).Milliseconds(),
	}), err
}

type kind struct{}

func (kind) Kind() spi.Kind { return Kind }
func (kind) Decode(raw []byte, reg *spi.Registry) (spi.Node, spi.Executor, error) {
	var dto struct {
		spi.BaseNodeDTO
		Data struct {
			Source string `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, nil, err
	}
	n := &scriptNode{BaseNode: dto.Base(reg), src: dto.Data.Source}
	return n, n, nil
}
func (kind) DecodeResult(raw []byte) (spi.AnyResult, error) {
	return spi.DecodeResultInto[Detail](raw, Kind)
}
func (kind) NewSkipped(n spi.Node, base spi.BaseResult) spi.AnyResult {
	return spi.NewSkippedResult[Detail](n, base) // replaces skipped.go type switch
}

func init() { registry.MustRegisterNode(kind{}) } // <-- the ONLY wiring
```

To make it available, blank-import the package in the wiring file (`_ "…/pkg/kinds/script"`). For an allow-listed registry, register explicitly: `reg.RegisterNode(script.New())`.

### (b) Add a custom extractor + assertion operator + dynamic var

```go
// ---- 1. EXTRACTOR that self-declares OutputType + compatible operators ----
// Replaces editing factory.go switch AND the hand-maintained
// pkg/compatibility/extractor_operator_compatibility.go map (verified dead-ish
// boilerplate — derivable from what an extractor knows about itself).
package cookie

import (
	"encoding/json"

	"github.com/nanostack-dev/echopoint-runner/pkg/operators"
	"github.com/nanostack-dev/echopoint-runner/pkg/registry"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type Extractor struct {
	Name string `json:"name"`
}

func (e Extractor) Type() spi.ExtractorType { return "cookie" }
func (e Extractor) Extract(rc spi.ResponseContext) (any, error) {
	if h, ok := rc.(spi.HeaderAccessor); ok {
		return parseCookie(h.GetHeader("Set-Cookie"), e.Name), nil
	}
	return "", nil
}

func init() {
	registry.MustRegisterExtractor(spi.ExtractorPlugin{
		Type:       "cookie",
		OutputType: operators.ValueTypeString, // self-declared compatibility (was central map)
		Operators:  []operators.OperatorType{operators.OperatorTypeEquals, operators.OperatorTypeContains},
		Decode: func(raw []byte) (spi.Extractor, error) {
			var e Extractor
			return e, json.Unmarshal(raw, &e)
		},
	})
}
```

```go
// ---- 2. OPERATOR as a first-class registered value (leaves pkg/node entirely) ----
// Today all 13 operators are inline lambdas in node.comparators (assertion.go:149).
// They move to pkg/operators as registered values, keyed by operators.OperatorType.
package operators // operators/length.go

func init() {
	MustRegister(OperatorPlugin{
		Type: "lengthEquals",
		Compare: func(actual, expected any) (bool, error) {
			return len(toString(actual)) == int(mustFloat(expected)), nil
		},
	})
}
// Assertion eval now calls reg.Operator(opType).Compare(actual, expected) instead
// of the closed comparators map. equals..between are registered the same way, so the
// dispatch table and the lenient toString/toFloat coercion live in ONE package.
```

```go
// ---- 3. DYNAMIC VAR (registry unchanged in behavior; chaining added) ----
package myvars

import "github.com/nanostack-dev/echopoint-runner/pkg/dynamicvars"

func init() {
	dynamicvars.Register(dynamicvars.Entry{
		Name:     "orderId",
		Category: "commerce",
		Gen: func(c *dynamicvars.Context, args []string) (string, error) {
			return newKSUID(), nil
		},
	})
}
// Resolvers compose at the API layer (fixes "no resolver composition"):
//   spi.ChainResolvers(tenantVars, dynamicvars.New(execID)) // first match wins
```

---

## 6. Backward compatibility

### JSON flow decode through the registry

`flow.Parse(data, reg)` replaces `ParseFromJSON` + `UnmarshalNode` + `UnmarshalExtractor` + `CompositeAssertion.UnmarshalJSON` dispatch — but each piece is *moved, not rewritten*:

```go
func flow.Parse(data []byte, reg *spi.Registry, opts ...ParseOption) (*flow.Flow, error)
```

1. Unmarshal the envelope (`name`/`version`/`edges`/`initialInputs`/`[]json.RawMessage` nodes) — same shape as `flow.go` today.
2. For each raw node, peek `{"type":...}`, dispatch to `reg.NodeKind(type).Decode(raw, reg)`. This registry lookup replaces `UnmarshalNode`'s switch; each kind owns its own DTO and the `RunWhen == "" → RunWhenOnSuccess` defaulting that is duplicated 3× in `unmarshal.go` today.
3. Inside `request`'s `Decode`, extractors/assertions decode via `reg.DecodeExtractor(raw)` and the **exact `assertionWire` decoder** (`assertion.go:29-105`) moves into `pkg/kinds/request` unchanged — snake_case `extractor_type`/`extractor_data` + camelCase fallback + nested `extractor` object + `operator_data.value` extraction, all preserved. Only request nodes carry assertions, so this lives with them.
4. `validateFlowReferences` runs as today, now over `[]spi.DataRef` instead of re-splitting strings.

### Preserved mechanisms (named, mapped 1:1)

| Behavior today | Preserved as |
|---|---|
| `on_success` then `always` two-phase loop | `engine.runOnSuccessPhase` / `runAlwaysPhase` — **logic unchanged**, types only. |
| `skipBlockedOnSuccessNodes` (cascade=false, leaves nodes in `remainingInputs`) | unchanged. |
| `skipFrontierAlwaysNodes` (cascade=true, unblocks downstream cleanup joins) | unchanged. |
| 5 skip-reason codes (`dependency_failed`, `dependency_skipped`, `missing_inputs`, `aborted_after_failure`, `not_reachable_after_main_phase`) | constants and `describeSkipCause` text **frozen** — locked by a golden test (see §7) so dashboards don't drift. |
| `createSkippedNodeResult` type switch | per-kind `NodeKind.NewSkipped` — produces the same `BaseExecutionResult`-shaped skipped result for each kind. |
| Observer event contract (`FlowStarted`/`NodeStarted`/`NodeFinished`/`FlowFinished`) | identical fields; `Result` type renamed, JSON unchanged. `MultiObserver`/`synchronizedObserver` kept. |
| Cycle/unreachable detection (`finalizeExecution`) and module cycle detection (`moduleExecutor` call-stack scan) | unchanged. |
| `FinalOutputs` flattening to `"nodeId.outputKey"` | unchanged (`propagateNodeOutputs`). |
| All three transports | three `Runner` configs (below). |

### Result decode (new — closes a real hole)

`FlowExecutionResult.ExecutionResults` is marshal-only today; `controlplane.FlowExecutionResultToPayload` double-marshals through `map[string]any`. The new `FlowResult.UnmarshalJSON` keys on `node_type` and calls `reg.NodeKind(kind).DecodeResult(raw)`, making the cloud/CLI round-trip type-safe. **Forward-compat:** an *unknown* `node_type` (e.g. a newer echopoint kind) degrades to a generic `BaseResult` rather than erroring — preserving the tolerance of today's `map[string]any` path.

### Wire guarantee

`BaseResult`/`FlowResult` JSON tags are byte-identical to `BaseExecutionResult`/`FlowExecutionResult` (`types.go:121-239`). `BaseResult` is **anonymously embedded** so it flattens (the `json:",inline"` from the proposal was invalid and is removed). Per the runner `AGENTS.md`, any event-shape change is checked against `echopoint/cmd/http/openapi.yaml` in the same session — here the verification is "no diff."

---

## 7. Migration plan

Ordered so each phase compiles and passes existing `engine_test.go` / `assertion_eval_test.go`. The biggest, lowest-risk wins ship **first and standalone**, before the SPI rewrite — this is the central graft from all three judges.

**Phase A — Ship the standalone wins (no SPI, no engine internals).**
- **A1. Builder.** Add `flow.NewBuilder` + `request.Node()/delay.Node()/module.Node()` producing today's concrete `*RequestNode` etc. Pure addition; zero engine change. Biggest ergonomics payoff.
- **A2. `context.Context` on Execute.** Change `AnyNode.Execute(ctx ExecutionContext)` → `Execute(ctx context.Context, ec ExecutionContext)`; thread the flow ctx from `runNode`; `RequestNode` derives its deadline from the passed ctx instead of `context.Background()` (`request_node.go:297`). Unlocks cancellation/timeouts.
- **A3. Executor middleware seam.** Wrap `AnyNode.Execute` in `runNode` with a `Middleware` chain (initially empty = no behavior change). Add `Retry`/`Timeout`/`Tracing`.
- **A4. Delete dead code.** `pkg/compatibility` and `pkg/assertions` (TODO scaffolding) are unimported outside themselves — remove. Derive compatibility from extractor self-declaration.
- **A5. Lock skip-reasons.** Add a golden test asserting the exact `skip_reason` codes + message text from `describeSkipCause` *before* any engine retype. This is the guardrail for Phases C-D.

*Breaks at A:* `Execute` signature (A2) — internal callers + the three node types. Everything else additive.

**Phase B — Registry behind the existing switches.**
- Add `pkg/registry`; have `UnmarshalNode`/`UnmarshalExtractor`/`comparators` **fall back** to it on a miss. Move the 13 operators from `node.comparators` into `pkg/operators` as registered values; `comparators` becomes a thin registry lookup. Finish the extractor registry: push `jsonPath`/`xmlPath`/`body` into self-registration so `factory.go`'s switch fully disappears. Run the full assertion + (now-derived) compatibility suites — must stay green.

**Phase C — `pkg/spi` + node kinds as plugins.**
- Introduce `pkg/spi`; start it as **type aliases** to the current `node.*` types (`AnyNode`, `ExecutionContext`, `AnyExecutionResult`, `OutputView`) so new code imports `spi` while old code compiles.
- Move `request_node.go`/`delay_node.go`/`module_node.go` to `pkg/kinds/{request,delay,module}`, each implementing `NodeKind` + self-registering. Delete the three switch cases and the `createSkippedNodeResult` switch (→ `NewSkipped`). Keep result shapes (`Result[*request.Detail]` marshals identically). Skip-reason golden test (A5) gates this.

**Phase D — Engine on `spi` only.**
- Retype `FlowEngine`/`executeNodes`/`runNode` to `spi.Node`/`spi.Executor`; build `spi.RunContext`. The two-phase loop and skip logic move **type-only**. ⚠ The engine uses `node.AnyNode` as a **map key** (`nodeEdgeOutput map[node.AnyNode][]node.AnyNode`); interface values remain usable as keys (pointer identity preserved), but this is the highest-risk swap — covered by `engine_test.go` cycle/unreachable/module-cycle cases plus A5.

**Phase E — Unify transports onto one `Runner`.**
- Reimplement `internal/ephemeral.Run`, `internal/runtime`, and echopoint `cloud_runner` as three `engine.Runner` configs. Collapse the verified triplication: `buildModuleResolver` (`ephemeral/runner.go:133`) vs `buildReferencedFlowResolver` (`runtime`) vs echopoint's resolver; `mergeInputs` (`ephemeral/runner.go:158`); `toPayload`/`FlowExecutionResultToPayload`. Observer events and payload shapes unchanged.

**Phase F — Cross-repo codegen + cleanup (sequenced against echopoint).**
- echopoint's generated `apigen/gen.go` aliases runner types (`type FlowNodeRunWhen = node.RunWhen`, `type NodeExecutionResultNodeType = node.Type`). Per the root `CLAUDE.md` "contract-first" rule, **change `echopoint/openapi.yaml` first, regenerate, then point the aliases at `spi`** — do not hand-edit generated files. Update echopoint `cloud_runner`/`cloud_execution_observer` and the CLI ephemeral launcher imports (`NewFlowEngine → NewRunner`, `MustAs[*RequestExecutionResult] → AsDetail[request.Detail]`). SSE JSON + DB persistence untouched.
- Remove the Phase-C `spi` aliases; `pkg/node` becomes thin or empty.

**What breaks, summarized:** `Execute` signature (A2); `NewFlowEngine/ExecuteFlowDefinition → NewRunner.Run` (E); result casts `As/MustAs → AsDetail` (F); the `node.Type`/`node.RunWhen` aliases in echopoint's generated code (F, via regen). **Shims:** the Phase-C type aliases keep the runner compiling mid-migration; the registry fallback (B) keeps JSON decode working while switches still exist.

---

## 8. Why this is more maintainable than today

Tied to concrete pain points from the map:

- **No more N-place edits to add a kind.** Today: `UnmarshalNode` switch (`unmarshal.go:17-47`), `createSkippedNodeResult` switch (`skipped.go:37-44`), per-kind result struct + `As/MustAs`, `InputSchema`/`OutputSchema`, `GetData` — ~7-10 sites, and `default: panic` for skipped results. After: one `NodeKind` file. The import-graph rule (core never imports a concrete kind) makes the seam *enforced*, not aspirational.

- **Cross-cutting concerns finally have a home.** There is currently **zero retry** and the only timeout is `RequestNode`'s hardcoded `defaultRequestTimeoutMs = 30000` built from `context.Background()` (`request_node.go:19,297`) — flow cancellation can't even reach a node because `Execute` takes no `context.Context`. The `context.Context` + `Executor` middleware split (shipped first, in Phase A) makes retry/backoff/timeout/tracing additive policy instead of forked node code.

- **In-code authoring exists.** `flow.go` is parse-only today; tests build maps→JSON→parse. The `Builder` (Phase A, standalone) removes that friction with no engine change.

- **Three switches collapse to registry lookups, dead code disappears.** `UnmarshalNode`, `UnmarshalExtractor` (already a *hybrid* switch+registry at `factory.go`), and the `comparators` map (`assertion.go:149`) become uniform registry dispatch. The unused `pkg/assertions` scaffolding and the hand-maintained `pkg/compatibility` map are deleted — compatibility is derived from extractor self-declaration.

- **Strings parsed once, not four times.** `parseDataRef` is re-run on every `assembleInputs`/`validateInputs`/`collectMissingInputs`/`describeSkipCause`. `DataRef` is parsed at flow-parse time into typed `InputSchema() []DataRef`.

- **The result round-trip stops being stringly-typed.** `ExecutionResults` is marshal-only and `controlplane` double-marshals via `map[string]any`; the new `DecodeResult` path makes cloud/CLI round-trips type-safe — without forcing generics through decode (the engine handles only `AnyResult`).

- **One transport instead of three drifting copies.** `buildModuleResolver`/`mergeInputs`/`toPayload` are triplicated across ephemeral, runtime, and echopoint cloud — one `Runner` with three configs removes the drift surface, with the SSE event contract preserved.

- **Risk is contained where it counts.** The subtlest, dashboard-load-bearing code (`skipFrontierAlwaysNodes`, `skipBlockedOnSuccessNodes`, `describeSkipCause`, `finalizeExecution`) is **not redesigned** — it moves type-only, gated by a skip-reason golden test written *before* the retype. The two genuinely valuable wins (Builder, middleware) ship in Phase A with no engine-internal or wire change, so most of the value lands before the riskiest phases begin.

**Net:** an extensibility/power redesign that makes adding behavior a one-file operation and gives cross-cutting concerns a structural home, while keeping every scheduling behavior, skip reason, observer event, and wire byte identical — and front-loading the high-value, low-risk pieces so the migration de-risks itself.

---

Key files this design evolves (all absolute):
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/node/types.go` (→ `pkg/spi`: `AnyNode`, `AnyExecutionResult`, `BaseExecutionResult`, `ExecutionContext`, `As`/`MustAs`)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/node/unmarshal.go` (→ registry `DecodeNode`)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/extractors/factory.go` (→ finish registry, drop switch)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/node/assertion.go` (→ operators registry; `assertionWire` decoder moves to `pkg/kinds/request`)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/operators/types.go` (→ registered `Operator` values)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/engine/engine.go` + `execution.go` (→ retype to `spi`, add middleware; phase/skip logic unchanged)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/engine/skipped.go` (→ `NodeKind.NewSkipped`; reason codes frozen)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/engine/observer.go` (→ `Result` type rename, JSON unchanged)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/node/request_node.go` (→ `pkg/kinds/request`; `context.Context`-driven deadline)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/flow/flow.go` (→ `Parse(data, reg)` + `Builder`)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/internal/ephemeral/runner.go` + `internal/runtime/runtime.go` (→ one `Runner`)
- `/Users/alexisgardin/Documents/NanostackProject/echopoint-runner/pkg/compatibility` + `pkg/assertions` (→ deleted)