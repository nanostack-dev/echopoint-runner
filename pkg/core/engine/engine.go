// Package engine is orchestration only. Given one flow, it schedules nodes in
// dependency order, runs each node's declared assertion/output post-step, and
// recurses for sub-flows. It has no per-node-type logic: every kind is dispatched
// the same way. It records each node's outcome in a result.FlowResult and keeps
// going past a failure (skipping dependents) rather than aborting. The engine
// also satisfies node.SubflowRunner, so composite nodes recurse back into it.
package engine

import (
	"context"
	"encoding/json"
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

// RunFlow executes a flow and returns its full result. The returned error is
// reserved for exceptional wiring issues; ordinary node failures are recorded in
// the result with Success=false.
func (e *Engine) RunFlow(ctx context.Context, f flow.Flow, inputs value.Map) (*result.FlowResult, error) {
	return e.run(ctx, f, inputs, true), nil
}

// run is the scheduler entrypoint: it builds the scheduler state, drains the
// ready queue, then finalizes (cycle detection + collected outputs). emit gates
// flow/node event emission (true only for the top-level flow, not sub-flows).
func (e *Engine) run(ctx context.Context, f flow.Flow, inputs value.Map, emit bool) *result.FlowResult {
	fr := &result.FlowResult{Success: true, Nodes: make(map[string]*result.NodeResult, len(f.Nodes))}
	e.emit(emit, Event{Type: spi.EventFlowStarted, Flow: fr})

	switch {
	case len(f.Nodes) == 0:
		fr.Success, fr.Error, fr.Code, fr.Outputs = false, "no nodes to execute", codeFlowValidation, value.Map{}
	default:
		if err := e.validateFlow(f, emit); err != nil {
			fr.Success, fr.Error, fr.Code, fr.Outputs = false, err.Error(), codeFlowValidation, value.Map{}
		} else {
			e.schedule(ctx, f, inputs, fr, emit)
		}
	}

	if fr.Success {
		e.emit(emit, Event{Type: spi.EventFlowCompleted, Flow: fr})
	} else {
		e.emit(emit, Event{Type: spi.EventFlowFailed, Flow: fr})
	}
	return fr
}

// validateFlow runs structural validation and, at the top level, walks the
// module reference graph to detect cycles and missing flows before executing
// anything (no side effects).
func (e *Engine) validateFlow(f flow.Flow, topLevel bool) error {
	if err := flow.Validate(f); err != nil {
		return err
	}
	if topLevel && e.resolve != nil {
		return e.walkModules(f, nil)
	}
	return nil
}

func (e *Engine) walkModules(f flow.Flow, stack []string) error {
	for _, n := range f.Nodes {
		if n.Kind != spi.KindModule {
			continue
		}
		var cfg struct {
			Body string `json:"body_flow_id"`
		}
		if err := json.Unmarshal(n.Raw, &cfg); err != nil || cfg.Body == "" {
			return fmt.Errorf("module %q: a body_flow_id is required", n.ID)
		}
		if slices.Contains(stack, cfg.Body) {
			return fmt.Errorf("module cycle detected: %s -> %s", strings.Join(stack, " -> "), cfg.Body)
		}
		child, ok := e.resolve(cfg.Body)
		if !ok {
			return fmt.Errorf("module %q references unknown flow %q", n.ID, cfg.Body)
		}
		if err := e.walkModules(child, append(stack, cfg.Body)); err != nil {
			return err
		}
	}
	return nil
}

// schedule drains the ready queue and finalizes cycle detection + outputs.
func (e *Engine) schedule(ctx context.Context, f flow.Flow, inputs value.Map, fr *result.FlowResult, emit bool) {
	s := newScheduler(f, inputs, fr)
	s.emit = emit
	for len(s.ready) > 0 {
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

// scheduler holds the mutable state of one run: the graph, the output store, and
// per-node terminal state (done/failed) plus dead routing edges.
type scheduler struct {
	nodeByID   map[string]flow.Node
	indeg      map[string]int
	succ       map[string][]string
	preds      map[string][]string
	store      map[string]value.Map
	done       map[string]bool
	failed     map[string]bool
	dead       map[string]map[string]bool
	mainFailed bool
	processed  int
	ready      []string
	fr         *result.FlowResult
	emit       bool
}

func newScheduler(f flow.Flow, inputs value.Map, fr *result.FlowResult) *scheduler {
	s := &scheduler{
		nodeByID: make(map[string]flow.Node, len(f.Nodes)),
		indeg:    make(map[string]int, len(f.Nodes)),
		succ:     make(map[string][]string, len(f.Nodes)),
		preds:    make(map[string][]string, len(f.Nodes)),
		store:    map[string]value.Map{"": inputs},
		done:     map[string]bool{},
		failed:   map[string]bool{},
		dead:     map[string]map[string]bool{},
		fr:       fr,
	}
	for _, n := range f.Nodes {
		s.nodeByID[n.ID] = n
		s.indeg[n.ID] = 0
	}
	for _, ed := range f.Edges {
		s.succ[ed.From] = append(s.succ[ed.From], ed.To)
		s.preds[ed.To] = append(s.preds[ed.To], ed.From)
		s.indeg[ed.To]++
	}
	for id, d := range s.indeg {
		if d == 0 {
			s.ready = append(s.ready, id)
		}
	}
	sort.Strings(s.ready)
	return s
}

// step runs or skips one ready node and records its outcome. A routing node
// marks the edges it routed away from as dead; a failed on_success node aborts
// the main phase; run_when=always nodes still run for cleanup.
func (e *Engine) step(ctx context.Context, s *scheduler, id string) {
	s.processed++
	fn := s.nodeByID[id]
	isAlways := runWhenOf(fn) == spi.RunWhenAlways

	if runIt, reason := classify(id, s.preds[id], s.done, s.failed, s.dead, s.mainFailed, isAlways); !runIt {
		nr := &result.NodeResult{ID: id, Kind: fn.Kind, Status: result.StatusSkipped, SkipReason: reason}
		s.fr.Nodes[id] = nr
		e.emit(s.emit, Event{Type: spi.EventNodeCompleted, NodeID: id, Node: nr})
		s.release(id)
		return
	}

	e.emit(s.emit, Event{Type: spi.EventNodeStarted, NodeID: id})
	res, assertions, err := e.runNode(ctx, fn, s.store)
	nr := &result.NodeResult{ID: id, Kind: fn.Kind, Assertions: assertions}
	if err != nil {
		nr.Status, nr.Error, nr.Code = result.StatusFailed, err.Error(), codeOrDefault(err)
		s.fr.Nodes[id] = nr
		s.failed[id] = true
		if !isAlways {
			s.mainFailed, s.fr.Success = true, false
		}
		e.emit(s.emit, Event{Type: spi.EventNodeFailed, NodeID: id, Node: nr})
		s.release(id)
		return
	}
	s.store[id] = res.Outputs
	s.done[id] = true
	nr.Status, nr.Outputs = result.StatusSuccess, res.Outputs
	s.fr.Nodes[id] = nr
	e.emit(s.emit, Event{Type: spi.EventNodeCompleted, NodeID: id, Node: nr})
	recordRouting(id, res.Routed, s.succ[id], s.dead)
	s.release(id)
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
func classify(
	id string, preds []string, done, failed map[string]bool,
	dead map[string]map[string]bool, mainFailed, isAlways bool,
) (bool, string) {
	if len(preds) == 0 {
		if !isAlways && mainFailed {
			return false, result.SkipNotReachable
		}
		return true, ""
	}
	live := false
	reason := result.SkipDependencySkipped
	for _, p := range preds {
		switch {
		case done[p] && !dead[p][id]:
			live = true
		case failed[p]:
			reason = result.SkipDependencyFailed
		case done[p] && dead[p][id]:
			if reason == result.SkipDependencySkipped {
				reason = result.SkipRoutedAway
			}
		}
	}
	if live {
		if !isAlways && mainFailed {
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
	ctx context.Context, fn flow.Node, store map[string]value.Map,
) (node.Result, assert.Results, error) {
	view := inputView(store)
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
			return res, nil, nil // node self-evaluated / routes / nothing to assert
		}
		results := assert.Run(res.Assert, b.Base.Assertions)
		if extracted := output.Extract(res.Assert, b.Base.Outputs); len(extracted) > 0 {
			if res.Outputs == nil {
				res.Outputs = value.Map{}
			}
			maps.Copy(res.Outputs, extracted)
		}
		if results.AnyFailed() {
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
	res := e.run(pushStack(ctx, flowID), child, in, false)
	if !res.Success {
		return nil, node.UserErrf("MODULE_FAILED", "child flow %q failed", flowID)
	}
	return res.Outputs, nil
}

// RunInline satisfies node.SubflowRunner for embedded body flows (loop, poll).
func (e *Engine) RunInline(ctx context.Context, f flow.Flow, in value.Map) (value.Map, error) {
	res := e.run(ctx, f, in, false)
	if !res.Success {
		return nil, node.UserErrf("SUBFLOW_FAILED", "inline body failed")
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

// runWhenOf peeks a node's run_when phase (default on_success).
func runWhenOf(fn flow.Node) spi.RunWhen {
	var head struct {
		RunWhen spi.RunWhen `json:"run_when"`
	}
	_ = json.Unmarshal(fn.Raw, &head)
	if head.RunWhen == "" {
		return spi.RunWhenOnSuccess
	}
	return head.RunWhen
}

func codeOrDefault(err error) string {
	if c := node.CodeOf(err); c != "" {
		return c
	}
	return "RUNNER_ERROR"
}

// recordRouting marks every successor a routing node did NOT route to as dead.
func recordRouting(id string, routed, successors []string, dead map[string]map[string]bool) {
	if routed == nil {
		return // ordinary node — all successors run; a routing node sets a (possibly empty) slice
	}
	taken := make(map[string]bool, len(routed))
	for _, t := range routed {
		taken[t] = true
	}
	for _, s := range successors {
		if !taken[s] {
			if dead[id] == nil {
				dead[id] = make(map[string]bool)
			}
			dead[id][s] = true
		}
	}
}

// inputView boxes a node's input context as a single Value: flow inputs at the
// top level, each upstream node's outputs nested under its id. assert/branch
// evaluate over this, addressing any already-executed node by path.
func inputView(store map[string]value.Map) value.Value {
	merged := make(map[string]any, len(store))
	for k, v := range store[""] { // flow inputs at top level
		merged[k] = v.Raw()
	}
	for nodeID, m := range store {
		if nodeID == "" {
			continue
		}
		merged[nodeID] = m.Value().Raw()
	}
	return value.Of(merged)
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
