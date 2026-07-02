// Package engine is orchestration only. Given one flow, it schedules nodes in
// dependency order, runs each node's declared assertion/output post-step, and
// recurses for sub-flows. It has no per-node-type logic: every kind is dispatched
// the same way. It records each node's outcome in a result.FlowResult and keeps
// going past a failure (skipping dependents) rather than aborting. The engine
// also satisfies node.SubflowRunner, so composite nodes recurse back into it.
package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/output"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/result"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/tmpl"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const codeFlowValidation = "FLOW_VALIDATION_FAILED"

// Engine runs flows.
type Engine struct {
	resolve    func(flowID string) (flow.Flow, bool)
	deps       node.Runtime
	observer   Observer
	middleware []Middleware
}

// Option configures an Engine at construction.
type Option func(*Engine)

// WithObserver attaches an execution-event observer (top-level flow only).
func WithObserver(o Observer) Option { return func(e *Engine) { e.observer = o } }

// WithMiddleware wraps every node's run-and-assert unit (e.g. Retry, Timeout).
func WithMiddleware(mw ...Middleware) Option {
	return func(e *Engine) { e.middleware = append(e.middleware, mw...) }
}

// New builds an engine. resolve looks up child flows by id (pass nil when the
// flow has no sub-flows). The engine wires itself in as the sub-flow runner.
func New(deps node.Runtime, resolve func(string) (flow.Flow, bool), opts ...Option) *Engine {
	e := &Engine{resolve: resolve, deps: deps}
	e.deps.Subflow = e
	for _, o := range opts {
		o(e)
	}
	return e
}

// RunFlow executes a flow and returns its full result. Node failures are
// recorded in the result (Success=false), not returned as an error.
func (e *Engine) RunFlow(ctx context.Context, f flow.Flow, inputs value.Map) *result.FlowResult {
	return e.run(ctx, f, inputs, e.observer, true)
}

// run is the scheduler entrypoint: it builds the scheduler state, drains the
// ready queue, then finalizes (cycle detection + collected outputs). obs is the
// event sink (nil for sub-flow runs); topLevel gates the module-graph validation
// (done once, at the outermost flow).
func (e *Engine) run(
	ctx context.Context, f flow.Flow, inputs value.Map, obs Observer, topLevel bool,
) *result.FlowResult {
	fr := &result.FlowResult{Success: true, Nodes: make(map[string]*result.NodeResult, len(f.Nodes))}
	emit(obs, Event{Type: spi.EventFlowStarted, Flow: fr})

	switch {
	case len(f.Nodes) == 0:
		fr.Success, fr.Error, fr.Code, fr.Outputs = false, "no nodes to execute", codeFlowValidation, value.Map{}
	default:
		if err := e.validateFlow(f, topLevel); err != nil {
			fr.Success, fr.Error, fr.Code, fr.Outputs = false, err.Error(), codeFlowValidation, value.Map{}
		} else {
			e.schedule(ctx, f, inputs, fr, obs)
		}
	}

	if fr.Success {
		emit(obs, Event{Type: spi.EventFlowCompleted, Flow: fr})
	} else {
		emit(obs, Event{Type: spi.EventFlowFailed, Flow: fr})
	}
	return fr
}

// validateFlow runs structural validation (edges), then generic capability
// validation (route targets have edges; referenced child flows exist and are
// acyclic). It decodes nodes and reads the Router/FlowReferencer capabilities
// they opted into — no per-node-type knowledge here.
func (e *Engine) validateFlow(f flow.Flow, topLevel bool) error {
	if err := flow.Validate(f); err != nil {
		return err
	}
	if err := validateTargets(f); err != nil {
		return err
	}
	if topLevel && e.resolve != nil {
		return e.walkRefs(f, nil)
	}
	return nil
}

// validateTargets checks every Router node's targets are real successor edges.
func validateTargets(f flow.Flow) error {
	hasRouter := false
	for _, n := range f.Nodes {
		if node.Routes(n.Kind) {
			hasRouter = true
			break
		}
	}
	if !hasRouter {
		return nil // no routing nodes — nothing to validate, skip building the edge index
	}
	succ := make(map[string]map[string]bool, len(f.Nodes))
	for _, ed := range f.Edges {
		if succ[ed.From] == nil {
			succ[ed.From] = map[string]bool{}
		}
		succ[ed.From][ed.To] = true
	}
	for _, n := range f.Nodes {
		if !node.Routes(n.Kind) {
			continue // only routing kinds have targets to validate — skip the decode
		}
		b, err := node.Decode(n.Kind, n.Raw)
		if err != nil {
			continue // decode errors surface at execution with a node result
		}
		for _, target := range b.Targets {
			if target != "" && !succ[n.ID][target] {
				return fmt.Errorf("node %q routes to %q but has no edge to it", n.ID, target)
			}
		}
	}
	return nil
}

// walkRefs walks the referenced-flow graph (FlowReferencer nodes) to detect
// cycles and unresolvable flows, with no side effects.
func (e *Engine) walkRefs(f flow.Flow, stack []string) error {
	for _, n := range f.Nodes {
		if !node.References(n.Kind) {
			continue // only flow-referencing kinds carry child refs — skip the decode
		}
		b, err := node.Decode(n.Kind, n.Raw)
		if err != nil {
			continue
		}
		for _, ref := range b.Refs {
			if slices.Contains(stack, ref) {
				return fmt.Errorf("flow cycle detected: %s -> %s", strings.Join(stack, " -> "), ref)
			}
			child, ok := e.resolve(ref)
			if !ok {
				return fmt.Errorf("node %q references unknown flow %q", n.ID, ref)
			}
			if recErr := e.walkRefs(child, append(stack, ref)); recErr != nil {
				return recErr
			}
		}
	}
	return nil
}

// schedule drains the ready queue and finalizes cycle detection + outputs.
func (e *Engine) schedule(ctx context.Context, f flow.Flow, inputs value.Map, fr *result.FlowResult, obs Observer) {
	s := newScheduler(f, inputs, fr)
	s.obs = obs
	for len(s.ready) > 0 {
		if err := ctx.Err(); err != nil {
			fr.Success, fr.Error, fr.Code = false, "flow cancelled: "+err.Error(), "CANCELLED"
			fr.Outputs = collect(s.store)
			return
		}
		id := s.ready[0]
		s.ready = s.ready[1:]
		e.step(ctx, s, id)
	}
	if s.processed != len(f.Nodes) {
		fr.Success, fr.Error, fr.Code = false, fmt.Sprintf(
			"cycle or unreachable nodes: processed %d of %d", s.processed, len(f.Nodes)), codeFlowValidation
	}
	fr.Outputs = collect(s.store)
}

// nodeState is a node's terminal state within one run.
type nodeState uint8

const (
	statePending nodeState = iota
	stateDone
	stateFailed
)

// scheduler holds the mutable state of one run: the graph, the output store, and
// per-node terminal state plus dead routing edges. The topology maps
// (indeg/succ/preds) and dead are allocated lazily — an edgeless body (common for
// loop/poll/module inline flows) and a branch-free flow skip them entirely.
type scheduler struct {
	nodeByID map[string]flow.Node
	indeg    map[string]int
	succ     map[string][]string
	preds    map[string][]string
	store    map[string]value.Map
	// view is the denormalized input context (flow inputs at top level, each
	// completed node's outputs nested under its id), maintained incrementally so
	// each node reads an O(1)-boxed view instead of rebuilding the whole store.
	view       map[string]any
	state      map[string]nodeState
	dead       map[string]map[string]bool // lazy: nil until a routing node records one
	mainFailed bool
	processed  int
	ready      []string
	fr         *result.FlowResult
	obs        Observer
}

func newScheduler(f flow.Flow, inputs value.Map, fr *result.FlowResult) *scheduler {
	s := &scheduler{
		nodeByID: make(map[string]flow.Node, len(f.Nodes)),
		store:    map[string]value.Map{"": mergeInputs(f.Inputs, inputs)},
		view:     make(map[string]any, len(f.Nodes)),
		state:    make(map[string]nodeState, len(f.Nodes)),
		fr:       fr,
	}
	for _, n := range f.Nodes {
		s.nodeByID[n.ID] = n
	}
	if len(f.Edges) == 0 {
		for _, n := range f.Nodes { // no edges: every node is a root
			s.ready = append(s.ready, n.ID)
		}
	} else {
		s.indeg = make(map[string]int, len(f.Nodes))
		s.succ = make(map[string][]string, len(f.Nodes))
		s.preds = make(map[string][]string, len(f.Nodes))
		for _, ed := range f.Edges {
			s.succ[ed.From] = append(s.succ[ed.From], ed.To)
			s.preds[ed.To] = append(s.preds[ed.To], ed.From)
			s.indeg[ed.To]++
		}
		for _, n := range f.Nodes { // roots = indegree 0 (iterate nodes for determinism)
			if s.indeg[n.ID] == 0 {
				s.ready = append(s.ready, n.ID)
			}
		}
	}
	sort.Strings(s.ready)
	for k, v := range s.store[""] { // seed the view with flow inputs at top level
		s.view[k] = v.Raw()
	}
	return s
}

// step runs or skips one ready node and records its outcome. A routing node
// marks the edges it routed away from as dead; a failed on_success node aborts
// the main phase; run_when=always nodes still run for cleanup.
func (e *Engine) step(ctx context.Context, s *scheduler, id string) {
	s.processed++
	fn := s.nodeByID[id]
	isAlways := fn.RunWhen == spi.RunWhenAlways

	if runIt, reason := s.classify(id, isAlways); !runIt {
		nr := &result.NodeResult{ID: id, Kind: fn.Kind, Status: result.StatusSkipped, SkipReason: reason}
		s.fr.Nodes[id] = nr
		emit(s.obs, Event{Type: spi.EventNodeCompleted, NodeID: id, Node: nr})
		s.release(id)
		return
	}

	emit(s.obs, Event{Type: spi.EventNodeStarted, NodeID: id})
	res, assertions, err := e.runNode(ctx, fn, value.Of(s.view))
	nr := &result.NodeResult{ID: id, Kind: fn.Kind, Assertions: assertions}
	if err != nil {
		code := node.CodeOf(err)
		if code == "" {
			code = "RUNNER_ERROR" // a genuine runner fault, not a user error
		}
		nr.Status, nr.Error, nr.Code = result.StatusFailed, err.Error(), code
		nr.Outputs = res.Outputs // keep what was produced (e.g. the body that failed an assertion)
		s.fr.Nodes[id] = nr
		s.state[id] = stateFailed
		if !isAlways {
			s.mainFailed, s.fr.Success = true, false
			if s.fr.Code == "" { // first main-phase failure wins the flow-level code/message
				s.fr.Code = code
				s.fr.Error = fmt.Sprintf("node %q: %s", id, err.Error())
			}
		}
		emit(s.obs, Event{Type: spi.EventNodeFailed, NodeID: id, Node: nr})
		s.release(id)
		return
	}
	s.store[id] = res.Outputs
	s.view[id] = res.Outputs.Value().Raw() // publish once; downstream nodes read it in O(1)
	s.state[id] = stateDone
	nr.Status, nr.Outputs = result.StatusSuccess, res.Outputs
	s.fr.Nodes[id] = nr
	emit(s.obs, Event{Type: spi.EventNodeCompleted, NodeID: id, Node: nr})
	s.recordRouting(id, res.Routed)
	s.release(id)
}

// mergeInputs overlays caller/runtime inputs on top of the flow's declared
// default inputs (the passed values win).
func mergeInputs(defaults, override value.Map) value.Map {
	if len(defaults) == 0 {
		return override
	}
	merged := make(value.Map, len(defaults)+len(override))
	maps.Copy(merged, defaults)
	maps.Copy(merged, override)
	return merged
}

// release decrements successors' in-degree and enqueues any that become ready.
func (s *scheduler) release(id string) {
	for _, succ := range s.succ[id] {
		s.indeg[succ]--
		if s.indeg[succ] == 0 {
			s.ready = append(s.ready, succ)
		}
	}
	sort.Strings(s.ready)
}

// classify decides whether a node runs, and if not, why it is skipped. A node
// runs when it has a live incoming edge (a succeeded predecessor whose edge was
// not routed away) — unless it is an on_success node and the main phase already
// failed, in which case cleanup is aborted. run_when=always nodes run for
// cleanup regardless of a main failure, as long as their inputs are available.
func (s *scheduler) classify(id string, isAlways bool) (bool, string) {
	preds := s.preds[id]
	if len(preds) == 0 {
		// A root has no dependency that could have failed. The old wave scheduler
		// ran every root in wave 0 — before any failure was observed — so a failing
		// independent sibling must not skip an unrelated root. Roots always run.
		return true, ""
	}
	live := false
	reason := result.SkipDependencySkipped
	for _, p := range preds {
		switch {
		case s.state[p] == stateDone && !s.dead[p][id]:
			live = true
		case s.state[p] == stateFailed:
			reason = result.SkipDependencyFailed
		case s.state[p] == stateDone && s.dead[p][id]:
			if reason == result.SkipDependencySkipped {
				reason = result.SkipRoutedAway
			}
		}
	}
	if live {
		if !isAlways && s.mainFailed {
			return false, result.SkipAbortedAfterFail
		}
		return true, ""
	}
	if isAlways {
		return false, result.SkipMissingInputs
	}
	return false, reason
}

// runNode resolves the node's templates against the output store, decodes it into
// typed config, runs it, and applies the uniform assertion/output post-step. It
// returns the node's result, the assertion results (nil for self-evaluating
// nodes), and an error (ASSERTION_FAILED when a declared assertion fails).
func (e *Engine) runNode(
	ctx context.Context, fn flow.Node, view value.Value,
) (node.Result, assert.Results, error) {
	resolved, err := tmpl.Resolve(fn.Raw, view, e.dynFunc())
	if err != nil {
		return node.Result{}, nil, node.UserErrf("INVALID_NODE_CONFIG", "template %s: %v", fn.Kind, err)
	}
	b, err := node.Decode(fn.Kind, resolved)
	if err != nil {
		return node.Result{}, nil, node.UserErrf("INVALID_NODE_CONFIG", "decode %s: %v", fn.Kind, err)
	}
	// The run-and-assert unit is what middleware (retry/timeout) wraps, so a
	// retry re-runs the assertion pass too.
	exec := func(ctx context.Context) (node.Result, assert.Results, error) {
		res, runErr := b.Run(ctx, view, e.deps)
		if runErr != nil {
			return node.Result{}, nil, runErr
		}
		if !res.Provided {
			return res, res.Assertions, nil // self-evaluated node's own results (may be nil)
		}
		results := assert.Run(res.Assert, b.Base.Assertions)
		if extracted := output.Extract(res.Assert, b.Base.Outputs); len(extracted) > 0 {
			if res.Outputs == nil {
				res.Outputs = value.Map{}
			}
			maps.Copy(res.Outputs, extracted)
		}
		if !results.AllPassed() {
			return res, results, node.UserErrf("ASSERTION_FAILED", "assertion failed on %s", b.Base.ID)
		}
		return res, results, nil
	}
	return chainMiddleware(exec, e.middleware)(ctx)
}

// RunSubflow satisfies node.SubflowRunner: it resolves a child flow by id and
// runs it, guarding against module cycles via the call stack carried in ctx.
func (e *Engine) RunSubflow(ctx context.Context, flowID string, in value.Map) (value.Map, error) {
	if e.resolve == nil {
		return nil, node.UserErrf("MODULE_FLOW_NOT_FOUND", "no sub-flow resolver configured")
	}
	if stackHas(ctx, flowID) {
		return nil, node.UserErrf("MODULE_CYCLE_DETECTED", "module cycle detected at %q", flowID)
	}
	child, ok := e.resolve(flowID)
	if !ok {
		return nil, node.UserErrf("MODULE_FLOW_NOT_FOUND", "child flow %q not found", flowID)
	}
	res := e.run(pushStack(ctx, flowID), child, in, nil, false)
	if !res.Success {
		code := res.Code
		if code == "" {
			code = "MODULE_FAILED"
		}
		return nil, node.UserErrf(code, "child flow %q failed: %s", flowID, res.Error)
	}
	return res.Outputs, nil
}

// RunInline satisfies node.SubflowRunner for embedded body flows (loop, poll).
func (e *Engine) RunInline(ctx context.Context, f flow.Flow, in value.Map) (value.Map, error) {
	res := e.run(ctx, f, in, nil, false)
	if !res.Success {
		code := res.Code
		if code == "" {
			code = "SUBFLOW_FAILED"
		}
		return nil, node.UserErrf(code, "inline body failed: %s", res.Error)
	}
	return res.Outputs, nil
}

// dynFunc adapts the runtime's dynamic-variable resolver for templating (nil
// when none is configured, disabling {{$...}} vars).
func (e *Engine) dynFunc() tmpl.DynFunc {
	if e.deps.Vars == nil {
		return nil
	}
	return e.deps.Vars.Resolve
}

// recordRouting marks every successor a routing node did NOT route to as dead.
// The dead map is allocated lazily here, so branch-free flows never pay for it.
func (s *scheduler) recordRouting(id string, routed []string) {
	if routed == nil {
		return // ordinary node — all successors run; a routing node sets a (possibly empty) slice
	}
	taken := make(map[string]bool, len(routed))
	for _, t := range routed {
		taken[t] = true
	}
	for _, succ := range s.succ[id] {
		if !taken[succ] {
			if s.dead == nil {
				s.dead = make(map[string]map[string]bool)
			}
			if s.dead[id] == nil {
				s.dead[id] = make(map[string]bool)
			}
			s.dead[id][succ] = true
		}
	}
}

// collect nests each node's outputs under its id, so results are accessed by
// path ("nodeID.key") uniformly — including a child flow's outputs.
func collect(store map[string]value.Map) value.Map {
	out := make(value.Map, len(store))
	for nodeID, m := range store {
		if nodeID == "" {
			continue // the synthetic flow-inputs node is not a result
		}
		out[nodeID] = m.Value()
	}
	return out
}

// stackKey carries the module call stack as request-scoped metadata — a
// legitimate context use (it guards recursion, it does not alter what a node
// computes).
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
