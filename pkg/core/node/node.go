// Package node is the node-authoring seam. A node is a typed Cfg plus a Run
// function; the framework owns everything else — JSON decode, the result
// envelope, the assertion/output post-step, and skip handling. Authors write
// only a Cfg struct (embedding Base) and one Run function, then Register it.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/output"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// UserErrf builds a coded user error (a spi.UserError) from a formatted message,
// so echopoint's spi.AsUserError sees new-core failures. Code is a stable string
// (REQUEST_FAILED, ASSERTION_FAILED, ...); a runner fault is a plain error.
func UserErrf(code, format string, args ...any) error {
	return spi.NewUserError(code, fmt.Sprintf(format, args...), nil)
}

// CodeOf returns a user error's stable code, or "" for a runner fault.
func CodeOf(err error) string {
	if ue, ok := spi.AsUserError(err); ok {
		return ue.Code
	}
	return ""
}

// IsUser reports whether err is a user-caused failure (vs a runner fault).
func IsUser(err error) bool {
	_, ok := spi.AsUserError(err)
	return ok
}

// Base is embedded by every node Cfg. It carries identity plus the declared
// assertions and outputs the framework evaluates after the node runs.
type Base struct {
	ID         string        `json:"id"`
	Name       string        `json:"display_name"`
	RunWhen    spi.RunWhen   `json:"run_when,omitempty"`
	Assertions []assert.Spec `json:"assertions,omitempty"`
	Outputs    []output.Spec `json:"outputs,omitempty"`
}

// base lets the framework read the common fields out of any Cfg that embeds
// Base. The method is unexported, so only types embedding node.Base satisfy the
// Register constraint — a sealed seam.
func (b Base) base() Base { return b }

type hasBase interface{ base() Base }

// Result is what a node's Run returns.
type Result struct {
	// Outputs are the named values the node produces for downstream nodes.
	Outputs value.Map
	// Assert is the value the node's declared assertions/outputs run against (a
	// response body, a computed map, ...) when Provided is true.
	Assert value.Value
	// Provided is set by nodes that expose a value for the framework's uniform
	// assertion/output post-step (request, set_variable, assert, loop). Nodes
	// that evaluate their own assertions (poll, sse) or route (branch) or have
	// none (delay, module) leave it false, and the engine skips the post-step.
	// This is explicit rather than inferred from a zero Assert, so asserting over
	// a JSON null still runs.
	Provided bool
	// Routed is set by routing nodes (branch): the successor ids execution was
	// routed to. The engine skips every other successor (and its subtree). Nil
	// for ordinary nodes — all successors run.
	Routed []string
}

// Run is the node-author function: typed cfg + the node's input context (flow
// inputs and upstream outputs, accessible by path) + runtime deps, result out.
// Most nodes ignore in — templating already filled their cfg; assert/branch use
// it to evaluate over inputs without naming them.
type Run[Cfg hasBase] func(ctx context.Context, cfg Cfg, in value.Value, rt Runtime) (Result, error)

// --- effect dependencies: the explicit, narrow capabilities a node may need ---

// HTTPDoer performs HTTP requests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Clock waits and reads wall time, respecting cancellation.
type Clock interface {
	Sleep(ctx context.Context, d time.Duration) error
	Now() time.Time
}

// SubflowRunner runs child flows and returns their outputs. The engine
// satisfies this; injecting it (rather than importing the engine) keeps the
// node package free of an import cycle and lets tests fake it. module references
// a flow by id; loop/poll run an inline body flow.
type SubflowRunner interface {
	RunSubflow(ctx context.Context, flowID string, in value.Map) (value.Map, error)
	RunInline(ctx context.Context, f flow.Flow, in value.Map) (value.Map, error)
}

// Runtime is the explicit dependency set handed to every node — genuine
// external effects only, no assert/extract/error sugar. A node touches only the
// fields it needs; a test builds a Runtime with just those. Vars resolves
// {{$dyn}} dynamic variables (spi.DynamicResolver, satisfied by pkg/dynamicvars).
type Runtime struct {
	HTTP    HTTPDoer
	Clock   Clock
	Subflow SubflowRunner
	Vars    spi.DynamicResolver
}

// --- capability interfaces (optional, implemented by a Cfg to opt into engine
// features generically, without the engine knowing the node type) ---

// FlowReferencer is implemented by node configs that reference child flows by id
// (module, ...), so the engine can validate references and detect cycles without
// special-casing the kind.
type FlowReferencer interface {
	ReferencedFlows() []string
}

// Router is implemented by node configs that route to specific successors
// (branch, ...), so the engine can validate that every target has an edge.
type Router interface {
	RouteTargets() []string
}

// --- registry ---

// Bound is a decoded node ready for the engine: its declared Base, its kind, a
// type-erased run closure holding the typed Cfg, and any capabilities the Cfg
// opted into (Refs/Targets), surfaced generically for validation.
type Bound struct {
	Base    Base
	Kind    spi.Kind
	Run     func(ctx context.Context, in value.Value, rt Runtime) (Result, error)
	Refs    []string // child flow ids (FlowReferencer), for cycle/existence validation
	Targets []string // route targets (Router), for branch-target validation
}

type decoder func(raw json.RawMessage) (Bound, error)

//nolint:gochecknoglobals // immutable-after-init node-kind registry
var registry = map[spi.Kind]decoder{}

// Register binds a kind to its typed Run. Cfg is inferred from fn; the closure
// erases it so differently-typed nodes share one registry. Call from an init().
// Registering a kind twice panics — that is a wiring bug, caught at load.
func Register[Cfg hasBase](kind spi.Kind, fn Run[Cfg]) {
	if _, dup := registry[kind]; dup {
		panic(fmt.Sprintf("node: duplicate registration for kind %q", kind))
	}
	registry[kind] = func(raw json.RawMessage) (Bound, error) {
		var cfg Cfg
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Bound{}, fmt.Errorf("decode %s node: %w", kind, err)
		}
		bound := Bound{
			Base: cfg.base(),
			Kind: kind,
			Run: func(ctx context.Context, in value.Value, rt Runtime) (Result, error) {
				return fn(ctx, cfg, in, rt)
			},
		}
		if fr, ok := any(cfg).(FlowReferencer); ok {
			bound.Refs = fr.ReferencedFlows()
		}
		if r, ok := any(cfg).(Router); ok {
			bound.Targets = r.RouteTargets()
		}
		return bound, nil
	}
}

// Decode turns a raw node definition into a Bound node via the registry.
func Decode(kind spi.Kind, raw json.RawMessage) (Bound, error) {
	dec, ok := registry[kind]
	if !ok {
		return Bound{}, fmt.Errorf("unknown node kind %q", kind)
	}
	return dec(raw)
}
