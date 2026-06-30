// Package node is the node-authoring seam. A node is a typed Cfg plus a Run
// function; the framework owns everything else — JSON decode, the result
// envelope, the assertion/output post-step, and skip handling. Authors write
// only a Cfg struct (embedding Base) and one Run function, then Register it.
package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/output"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// ErrUser tags a failure as caused by the flow definition or the target system
// (bad config, a 500 from the called API) rather than a runner fault. Wrap it
// with %w; the engine classifies on it.
var ErrUser = errors.New("user error")

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
	// response body, a computed map, ...). Leave it None when the node already
	// evaluated its own assertions in a loop (poll, sse); the engine then skips
	// the post-step.
	Assert value.Value
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

// Resolver resolves {{$dyn}} dynamic variables.
type Resolver interface {
	Resolve(name string, args []string) (string, error)
}

// Runtime is the explicit dependency set handed to every node — genuine
// external effects only, no assert/extract/error sugar. A node touches only the
// fields it needs; a test builds a Runtime with just those.
type Runtime struct {
	HTTP    HTTPDoer
	Clock   Clock
	Subflow SubflowRunner
	Vars    Resolver
}

// --- registry ---

// Bound is a decoded node ready for the engine: its declared Base, its kind, and
// a type-erased run closure that already holds the typed Cfg.
type Bound struct {
	Base Base
	Kind spi.Kind
	Run  func(ctx context.Context, in value.Value, rt Runtime) (Result, error)
}

type decoder func(raw json.RawMessage) (Bound, error)

//nolint:gochecknoglobals // immutable-after-init node-kind registry
var registry = map[spi.Kind]decoder{}

// Register binds a kind to its typed Run. Cfg is inferred from fn; the closure
// erases it so differently-typed nodes share one registry. Call from an init().
func Register[Cfg hasBase](kind spi.Kind, fn Run[Cfg]) {
	registry[kind] = func(raw json.RawMessage) (Bound, error) {
		var cfg Cfg
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Bound{}, fmt.Errorf("decode %s node: %w", kind, err)
		}
		b := cfg.base()
		return Bound{
			Base: b,
			Kind: kind,
			Run: func(ctx context.Context, in value.Value, rt Runtime) (Result, error) {
				return fn(ctx, cfg, in, rt)
			},
		}, nil
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
