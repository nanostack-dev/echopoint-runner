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
	TypeRequest = spi.KindRequest
	TypeDelay   = spi.KindDelay
	TypeModule  = spi.KindModule
	TypePoll    = spi.KindPoll
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
