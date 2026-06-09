package spi

import (
	"fmt"
	"time"
)

// AnyResult is the interface for all node execution results (polymorphic).
type AnyResult interface {
	GetNodeID() string
	GetDisplayName() string
	GetNodeType() Kind
	GetInputs() map[string]any
	GetOutputs() map[string]any
	GetError() error
	GetExecutedAt() time.Time

	// isExecutionResult prevents external implementations; embed
	// BaseExecutionResult to satisfy it.
	isExecutionResult()
}

// BaseExecutionResult provides common fields for all execution results. Its JSON
// tags are a wire contract consumed by echopoint.
type BaseExecutionResult struct {
	NodeID        string         `json:"node_id"`
	DisplayName   string         `json:"display_name"`
	NodeType      Kind           `json:"node_type"`
	RunWhen       RunWhen        `json:"run_when,omitempty"`
	Inputs        map[string]any `json:"inputs"`
	Outputs       map[string]any `json:"outputs"`
	Error         error          `json:"-"` // Don't serialize Go error
	ErrorCode     *string        `json:"error_code,omitempty"`
	ErrorMsg      *string        `json:"error_message,omitempty"`
	SkipReason    *string        `json:"skip_reason,omitempty"`
	MissingInputs []string       `json:"missing_inputs,omitempty"`
	ExecutedAt    time.Time      `json:"executed_at"`
}

// GetNodeID returns the node ID.
func (b *BaseExecutionResult) GetNodeID() string { return b.NodeID }

// GetDisplayName returns the node display name.
func (b *BaseExecutionResult) GetDisplayName() string { return b.DisplayName }

// GetNodeType returns the node type.
func (b *BaseExecutionResult) GetNodeType() Kind { return b.NodeType }

// GetInputs returns the inputs map.
func (b *BaseExecutionResult) GetInputs() map[string]any { return b.Inputs }

// GetOutputs returns the outputs map.
func (b *BaseExecutionResult) GetOutputs() map[string]any { return b.Outputs }

// GetError returns the error if any.
func (b *BaseExecutionResult) GetError() error { return b.Error }

// GetExecutedAt returns the execution timestamp.
func (b *BaseExecutionResult) GetExecutedAt() time.Time { return b.ExecutedAt }

func (b *BaseExecutionResult) isExecutionResult() {}

// AssertionResult records the outcome of evaluating a single node assertion,
// captured whether it passed or failed so the full result can be reported.
type AssertionResult struct {
	Index     int    `json:"index"`
	Extractor string `json:"extractor"`
	Operator  string `json:"operator"`
	Expected  any    `json:"expected"`
	Actual    any    `json:"actual"`
	Passed    bool   `json:"passed"`
	Error     string `json:"error,omitempty"`
}

// As safely casts an AnyResult to a concrete result type T
// (e.g. As[*request.RequestExecutionResult](result)). It reports false instead
// of panicking when the dynamic type does not match.
func As[T AnyResult](result AnyResult) (T, bool) {
	concrete, ok := result.(T)
	return concrete, ok
}

// MustAs casts an AnyResult to a concrete result type T, panicking when the
// dynamic type does not match. Use only where the type is an invariant.
func MustAs[T AnyResult](result AnyResult) T {
	concrete, ok := As[T](result)
	if !ok {
		var want T
		panic(fmt.Sprintf("expected execution result of type %T but got %T", want, result))
	}
	return concrete
}

// FlowExecutionResult contains the complete trace of a flow execution.
type FlowExecutionResult struct {
	ExecutionResults map[string]AnyResult `json:"execution_results"` // Polymorphic results!
	FinalOutputs     map[string]any       `json:"final_outputs"`     // All outputs flattened ("nodeId.outputKey": value)
	Success          bool                 `json:"success"`
	Error            error                `json:"-"`
	ErrorCode        *string              `json:"error_code,omitempty"`
	ErrorMsg         *string              `json:"error_message,omitempty"`
	DurationMS       int64                `json:"duration_ms"`
}
