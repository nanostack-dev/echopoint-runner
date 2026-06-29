package node

import (
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// OutputView is re-exported from spi (the L0 contract). Alias kept for back-compat.
type OutputView = spi.OutputView

// AnyNode is the full authoring/engine view of a node: the capability-agnostic
// core (spi.Node) plus the assertion and output accessors, which carry concrete
// extractor decode/eval behavior and therefore stay in this package.
type AnyNode interface {
	spi.Node

	// GetAssertions returns the list of assertions to validate during execution.
	// Assertions should be evaluated before extractions.
	GetAssertions() []CompositeAssertion

	// GetOutputs returns the list of extractions to perform on the response/data.
	// Outputs should be evaluated after assertions pass.
	GetOutputs() []Output
}

// ResolvedModuleFlow is re-exported from spi. Alias kept for back-compat.
type ResolvedModuleFlow = spi.ResolvedModuleFlow

// ModuleResolver is re-exported from spi. Alias kept for back-compat.
type ModuleResolver = spi.ModuleResolver

// ModuleExecutionRequest is re-exported from spi. Alias kept for back-compat.
type ModuleExecutionRequest = spi.ModuleExecutionRequest

// ModuleExecutor is re-exported from spi. Alias kept for back-compat.
type ModuleExecutor = spi.ModuleExecutor

// TypeNode is a node with typed data.
type TypeNode[T any] interface {
	AnyNode
	GetData() T
}

// Type is re-exported from spi (was node.Type, now spi.Kind). Alias kept for back-compat.
type Type = spi.Kind

// Built-in node kinds (re-exported from spi).
const (
	TypeRequest     = spi.KindRequest
	TypeDelay       = spi.KindDelay
	TypeModule      = spi.KindModule
	TypeSetVariable = spi.KindSetVariable
	TypeLoop        = spi.KindLoop
	TypePoll        = spi.KindPoll
	TypeAssert      = spi.KindAssert
	TypeBranch      = spi.KindBranch
	TypeSse         = spi.KindSse
)

// RunWhen is re-exported from spi. Alias kept for back-compat.
type RunWhen = spi.RunWhen

// RunWhen phases (re-exported from spi).
const (
	RunWhenOnSuccess = spi.RunWhenOnSuccess
	RunWhenAlways    = spi.RunWhenAlways
)

// ExecutionContext is re-exported from spi. Alias kept for back-compat.
type ExecutionContext = spi.ExecutionContext

// DynamicResolver is re-exported from spi. Alias kept for back-compat.
type DynamicResolver = spi.DynamicResolver

// AnyExecutionResult is re-exported from spi (was node.AnyExecutionResult, now
// spi.AnyResult). Alias kept for back-compat.
type AnyExecutionResult = spi.AnyResult

// BaseExecutionResult is re-exported from spi. Alias kept for back-compat.
type BaseExecutionResult = spi.BaseExecutionResult

// AssertionResult is re-exported from spi. Alias kept for back-compat.
type AssertionResult = spi.AssertionResult

// RequestExecutionResult stores HTTP request node execution data.
type RequestExecutionResult struct {
	BaseExecutionResult

	// HTTP Request fields
	RequestMethod  string            `json:"request_method"`
	RequestURL     string            `json:"request_url"`
	RequestHeaders map[string]string `json:"request_headers"`
	RequestBody    any               `json:"request_body,omitempty"`

	// HTTP Response fields
	ResponseStatusCode int                 `json:"response_status_code"`
	ResponseHeaders    map[string][]string `json:"response_headers"`
	ResponseBody       []byte              `json:"response_body,omitempty"`
	ResponseBodyParsed any                 `json:"response_body_parsed,omitempty"`

	// AssertionResults now lives on the embedded BaseExecutionResult so the
	// engine-level assertion pass fills it uniformly; the wire shape (the
	// "assertion_results" tag) is unchanged.

	// assertionCtx is the ResponseContext the engine's assertion/output pass
	// evaluates against. It is built during Execute (on a successful HTTP
	// exchange) and exposed via AssertionContext(); it is never serialized.
	assertionCtx extractors.ResponseContext

	// Timing
	DurationMs int64 `json:"duration_ms"`
}

// AssertionContext exposes the ResponseContext the engine-level assertion/output
// pass evaluates against. It satisfies AssertionContextProvider; a nil context
// (e.g. an error result built before the HTTP exchange completed) signals the
// engine to skip the pass.
func (r *RequestExecutionResult) AssertionContext() extractors.ResponseContext {
	return r.assertionCtx
}

// DelayExecutionResult stores delay node execution data.
type DelayExecutionResult struct {
	BaseExecutionResult

	DelayMs    int64     `json:"delay_ms"`
	DelayUntil time.Time `json:"delay_until"`
}

// ModuleExecutionResult stores nested module execution data.
type ModuleExecutionResult struct {
	BaseExecutionResult

	FlowID            string         `json:"flow_id"`
	ChildFinalOutputs map[string]any `json:"child_final_outputs,omitempty"`
	DurationMs        int64          `json:"duration_ms"`
}

// SetVariableExecutionResult stores set-variable node execution data. The
// computed named values are exposed both as the node Outputs (via the embedded
// base) and as the engine sees them; DurationMs records resolution time.
type SetVariableExecutionResult struct {
	BaseExecutionResult

	DurationMs int64 `json:"duration_ms"`
}

// AssertionContext exposes the computed variables map (the node Outputs) as the
// ResponseContext the engine-level assertion/output pass evaluates against. This
// gives set-variable nodes free assertions over their resolved variables,
// satisfying AssertionContextProvider. A nil Outputs map (e.g. an error result)
// signals the engine to skip the pass.
func (r *SetVariableExecutionResult) AssertionContext() extractors.ResponseContext {
	if r.Outputs == nil {
		return nil
	}
	return extractors.NewValueResponseContext(r.Outputs)
}

// LoopExecutionResult stores foreach loop node execution data.
type LoopExecutionResult struct {
	BaseExecutionResult

	// Iterations is the number of body executions that were attempted
	// (after applying any max_iterations cap).
	Iterations int   `json:"iterations"`
	DurationMs int64 `json:"duration_ms"`

	// assertionCtx is the ResponseContext the engine's assertion/output pass
	// evaluates against. It wraps the loop's aggregate outputs ({results, count})
	// so users can assert on the collected iteration results; it is never
	// serialized. It is built during Execute on the success path only.
	assertionCtx extractors.ResponseContext
}

// AssertionContext exposes the ResponseContext the engine-level assertion/output
// pass evaluates against — the loop's aggregate outputs ({results, count}). It
// satisfies AssertionContextProvider; a nil context (e.g. an error result built
// before the loop completed) signals the engine to skip the pass.
func (r *LoopExecutionResult) AssertionContext() extractors.ResponseContext {
	return r.assertionCtx
}

// PollExecutionResult stores poll-until node execution data. The poll node
// re-runs an inline body sub-flow on an interval until all of its exit-condition
// assertions pass on a single attempt, or it exhausts its attempt/deadline budget.
type PollExecutionResult struct {
	BaseExecutionResult

	// Attempts is the number of body executions performed (the attempt on which
	// the poll succeeded, or the total attempts made before giving up).
	//
	// The exit-condition evaluation from the final attempt (the passing attempt on
	// success, or the last attempt on failure) is recorded in the promoted
	// BaseExecutionResult.AssertionResults field.
	Attempts   int   `json:"attempts"`
	DurationMs int64 `json:"duration_ms"`
}

// AssertExecutionResult stores assert node execution data: the value asserted
// over and the full set of assertion outcomes captured during evaluation.
type AssertExecutionResult struct {
	BaseExecutionResult

	// AssertionResults now lives on the embedded BaseExecutionResult so the
	// engine-level assertion pass fills it uniformly (wire tag "assertion_results"
	// unchanged).
	//
	// assertionCtx is the ResponseContext the engine's assertion/output pass
	// evaluates against. It is built during Execute from the resolved target
	// value and exposed via AssertionContext(); it is never serialized.
	assertionCtx extractors.ResponseContext

	DurationMs int64 `json:"duration_ms"`
}

// AssertionContext exposes the ResponseContext the engine-level assertion/output
// pass evaluates against. It satisfies AssertionContextProvider; a nil context
// (e.g. an error result built before the target was resolved) signals the
// engine to skip the pass.
func (r *AssertExecutionResult) AssertionContext() extractors.ResponseContext {
	return r.assertionCtx
}

// BranchExecutionResult stores value-based routing decision data. It implements
// spi.RoutingResult so the engine skips the successor subtrees the branch routed
// away from.
type BranchExecutionResult struct {
	BaseExecutionResult

	// MatchedTarget is the successor node ID execution was routed to, or "" when
	// no case matched and no default was configured.
	MatchedTarget string `json:"matched_target"`
	// RoutedTargetIDs holds the chosen successor node IDs (one element when a
	// case/default matched, empty otherwise).
	RoutedTargetIDs []string `json:"routed_targets"`
	DurationMs      int64    `json:"duration_ms"`
}

// RoutedTargets implements spi.RoutingResult, returning the successor node IDs
// this branch routed execution to.
func (r *BranchExecutionResult) RoutedTargets() []string {
	return r.RoutedTargetIDs
}

// SseExecutionResult stores SSE (Server-Sent Events) node execution data.
type SseExecutionResult struct {
	BaseExecutionResult

	// RequestMethod and RequestURL capture the resolved connection details.
	RequestMethod string `json:"request_method"`
	RequestURL    string `json:"request_url"`

	// Events holds every dispatched event's parsed data (JSON when the data was
	// valid JSON, otherwise the raw string), in arrival order.
	Events []any `json:"events"`
	// EventCount is len(Events).
	EventCount int `json:"event_count"`

	// AssertionResults (every assertion evaluated across all events, pass or
	// fail, with Index repurposed to carry the event index) is inherited from the
	// embedded BaseExecutionResult so the wire shape stays uniform across nodes.

	// StopReason records why streaming stopped (max_events, completion_event,
	// timeout, eof, assertion_failure).
	StopReason string `json:"stop_reason,omitempty"`

	// Timing
	DurationMs int64 `json:"duration_ms"`
}

// As safely casts an AnyExecutionResult to a concrete result type T
// (e.g. As[*RequestExecutionResult](result)). It reports false instead of
// panicking when the dynamic type does not match. Delegates to spi.As.
func As[T AnyExecutionResult](result AnyExecutionResult) (T, bool) {
	return spi.As[T](result)
}

// MustAs casts an AnyExecutionResult to a concrete result type T, panicking when
// the dynamic type does not match. Delegates to spi.MustAs.
func MustAs[T AnyExecutionResult](result AnyExecutionResult) T {
	return spi.MustAs[T](result)
}

// FlowExecutionResult is re-exported from spi. Alias kept for back-compat.
type FlowExecutionResult = spi.FlowExecutionResult
