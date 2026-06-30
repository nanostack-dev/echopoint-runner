// Package engine is orchestration only. Given one flow, it schedules nodes in
// dependency order, runs each node's declared assertion/output post-step, and
// recurses for sub-flows. It has no per-node-type logic: every kind is dispatched
// the same way. The engine also satisfies node.SubflowRunner, so composite nodes
// (module/poll/loop) recurse back into it.
package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/output"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/tmpl"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

// Engine runs flows.
type Engine struct {
	resolve func(flowID string) (flow.Flow, bool)
	deps    node.Runtime
}

// New builds an engine. resolve looks up child flows by id (pass nil when the
// flow has no sub-flows). The engine wires itself in as the sub-flow runner.
func New(deps node.Runtime, resolve func(string) (flow.Flow, bool)) *Engine {
	e := &Engine{resolve: resolve, deps: deps}
	e.deps.Subflow = e
	return e
}

// RunFlow executes a flow and returns its outputs flattened as "nodeID.key".
func (e *Engine) RunFlow(ctx context.Context, f flow.Flow, inputs value.Map) (value.Map, error) {
	nodeByID := make(map[string]flow.Node, len(f.Nodes))
	indeg := make(map[string]int, len(f.Nodes))
	succ := make(map[string][]string, len(f.Nodes))
	for _, n := range f.Nodes {
		nodeByID[n.ID] = n
		indeg[n.ID] = 0
	}
	for _, ed := range f.Edges {
		succ[ed.From] = append(succ[ed.From], ed.To)
		indeg[ed.To]++
	}

	bus := make(map[string]value.Map, len(f.Nodes)+1)
	bus[""] = inputs // flow inputs live under the synthetic empty-id node
	ready := make([]string, 0, len(f.Nodes))
	for id, d := range indeg {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	ran := 0
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]

		res, err := e.runNode(ctx, nodeByID[id], bus)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", id, err)
		}
		bus[id] = res.Outputs
		ran++

		for _, s := range succ[id] {
			indeg[s]--
			if indeg[s] == 0 {
				ready = append(ready, s)
			}
		}
		sort.Strings(ready)
	}
	if ran != len(f.Nodes) {
		return nil, fmt.Errorf("cycle or unreachable nodes: ran %d of %d", ran, len(f.Nodes))
	}
	return collect(bus), nil
}

// runNode resolves the node's templates against the bus, decodes it into typed
// config, runs it, and applies the uniform assertion/output post-step. This is
// the entire per-node lifecycle — identical for every kind.
func (e *Engine) runNode(ctx context.Context, fn flow.Node, bus map[string]value.Map) (node.Result, error) {
	resolved, err := tmpl.Resolve(fn.Raw, tmpl.Bus(bus))
	if err != nil {
		return node.Result{}, err
	}
	b, err := node.Decode(fn.Kind, resolved)
	if err != nil {
		return node.Result{}, err
	}
	res, err := b.Run(ctx, e.deps)
	if err != nil {
		return node.Result{}, err
	}
	if res.Assert.IsZero() {
		return res, nil // node self-evaluated (or has nothing to assert)
	}
	results := assert.Run(res.Assert, b.Base.Assertions)
	if extracted := output.Extract(res.Assert, b.Base.Outputs); len(extracted) > 0 {
		if res.Outputs == nil {
			res.Outputs = value.Map{}
		}
		maps.Copy(res.Outputs, extracted)
	}
	if results.AnyFailed() {
		return node.Result{}, fmt.Errorf("assertion failed on %s: %w", b.Base.ID, node.ErrUser)
	}
	return res, nil
}

// RunSubflow satisfies node.SubflowRunner: it resolves a child flow by id and
// runs it, guarding against module cycles via the call stack carried in ctx.
func (e *Engine) RunSubflow(ctx context.Context, flowID string, in value.Map) (value.Map, error) {
	if e.resolve == nil {
		return nil, fmt.Errorf("no sub-flow resolver configured: %w", node.ErrUser)
	}
	if stackHas(ctx, flowID) {
		return nil, fmt.Errorf("module cycle detected at %q: %w", flowID, node.ErrUser)
	}
	child, ok := e.resolve(flowID)
	if !ok {
		return nil, fmt.Errorf("child flow %q not found: %w", flowID, node.ErrUser)
	}
	return e.RunFlow(pushStack(ctx, flowID), child, in)
}

// RunInline satisfies node.SubflowRunner for embedded body flows (loop, poll).
func (e *Engine) RunInline(ctx context.Context, f flow.Flow, in value.Map) (value.Map, error) {
	return e.RunFlow(ctx, f, in)
}

// collect nests each node's outputs under its id, so results are accessed by
// path ("nodeID.key") uniformly — including a child flow's outputs when a
// composite node asserts over them.
func collect(bus map[string]value.Map) value.Map {
	out := make(value.Map, len(bus))
	for nodeID, m := range bus {
		if nodeID == "" {
			continue // the synthetic flow-inputs node is not a result
		}
		out[nodeID] = m.Value()
	}
	return out
}

// stackKey carries the module call stack as request-scoped metadata — a
// legitimate context use (it does not alter what a node computes, only guards
// recursion).
type stackKey struct{}

func pushStack(ctx context.Context, flowID string) context.Context {
	prev, _ := ctx.Value(stackKey{}).([]string)
	next := append(append([]string{}, prev...), flowID)
	return context.WithValue(ctx, stackKey{}, next)
}

func stackHas(ctx context.Context, flowID string) bool {
	prev, _ := ctx.Value(stackKey{}).([]string)
	return slices.Contains(prev, flowID)
}
