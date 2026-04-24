package node

import "time"

type OutputView interface {
	HasNode(nodeID string) bool
	Get(nodeID, outputKey string) (interface{}, bool)
	// Node returns a defensive copy of the requested node outputs.
	Node(nodeID string) map[string]interface{}
}

type AnyNode interface {
	GetID() string
	GetDisplayName() string
	GetType() Type
	GetRunWhen() RunWhen
	InputSchema() []string

	// OutputSchema defines what this node produces
	// Examples: []string{"statusCode", "userId", "responseBody"}
	OutputSchema() []string

	// GetAssertions returns the list of assertions to validate during execution
	// Assertions should be evaluated before extractions
	GetAssertions() []CompositeAssertion

	// GetOutputs returns the list of extractions to perform on the response/data
	// Outputs should be evaluated after assertions pass
	GetOutputs() []Output

	// Execute performs the node's action with provided inputs
	// Returns AnyExecutionResult (polymorphic) containing outputs and execution metadata
	// Error indicates execution failure
	Execute(ctx ExecutionContext) (AnyExecutionResult, error)
}

type TypeNode[T any] interface {
	AnyNode
	GetData() T
}

type Type string

const (
	TypeRequest Type = "request"
	TypeDelay   Type = "delay"
)

type RunWhen string

const (
	RunWhenOnSuccess RunWhen = "on_success"
	RunWhenAlways    RunWhen = "always"
)

// ExecutionContext provides inputs and context for a node's execution.
type ExecutionContext struct {
	// Inputs contains all the data this node declared it needs in InputSchema()
	// Keys are in format "nodeId.outputKey" (e.g., "create-user.userId")
	Inputs map[string]interface{}
	// AllOutputs exposes a read-only snapshot of outputs from nodes that completed
	// before the current scheduling batch started.
	AllOutputs OutputView
}

// AnyExecutionResult is the interface for all execution results (polymorphic).
type AnyExecutionResult interface {
	GetNodeID() string
	GetDisplayName() string
	GetNodeType() Type
	GetInputs() map[string]interface{}
	GetOutputs() map[string]interface{}
	GetError() error
	GetExecutedAt() time.Time

	// Internal method to prevent external implementations
	isExecutionResult()
}

// BaseExecutionResult provides common fields for all execution results.
type BaseExecutionResult struct {
	NodeID        string                 `json:"node_id"`
	DisplayName   string                 `json:"display_name"`
	NodeType      Type                   `json:"node_type"`
	RunWhen       RunWhen                `json:"run_when,omitempty"`
	Inputs        map[string]interface{} `json:"inputs"`
	Outputs       map[string]interface{} `json:"outputs"`
	Error         error                  `json:"-"` // Don't serialize Go error
	ErrorCode     *string                `json:"error_code,omitempty"`
	ErrorMsg      *string                `json:"error_message,omitempty"`
	SkipReason    *string                `json:"skip_reason,omitempty"`
	MissingInputs []string               `json:"missing_inputs,omitempty"`
	ExecutedAt    time.Time              `json:"executed_at"`
}

// GetNodeID returns the node ID.
func (b *BaseExecutionResult) GetNodeID() string { return b.NodeID }

// GetDisplayName returns the node display name.
func (b *BaseExecutionResult) GetDisplayName() string { return b.DisplayName }

// GetNodeType returns the node type.
func (b *BaseExecutionResult) GetNodeType() Type { return b.NodeType }

// GetInputs returns the inputs map.
func (b *BaseExecutionResult) GetInputs() map[string]interface{} { return b.Inputs }

// GetOutputs returns the outputs map.
func (b *BaseExecutionResult) GetOutputs() map[string]interface{} { return b.Outputs }

// GetError returns the error if any.
func (b *BaseExecutionResult) GetError() error { return b.Error }

// GetExecutedAt returns the execution timestamp.
func (b *BaseExecutionResult) GetExecutedAt() time.Time { return b.ExecutedAt }

func (b *BaseExecutionResult) isExecutionResult() {}

// RequestExecutionResult stores HTTP request node execution data.
type RequestExecutionResult struct {
	BaseExecutionResult

	// HTTP Request fields
	RequestMethod  string            `json:"request_method"`
	RequestURL     string            `json:"request_url"`
	RequestHeaders map[string]string `json:"request_headers"`
	RequestBody    interface{}       `json:"request_body,omitempty"`

	// HTTP Response fields
	ResponseStatusCode int                 `json:"response_status_code"`
	ResponseHeaders    map[string][]string `json:"response_headers"`
	ResponseBody       []byte              `json:"response_body,omitempty"`
	ResponseBodyParsed interface{}         `json:"response_body_parsed,omitempty"`

	// Timing
	DurationMs int64 `json:"duration_ms"`
}

// DelayExecutionResult stores delay node execution data.
type DelayExecutionResult struct {
	BaseExecutionResult

	DelayMs    int64     `json:"delay_ms"`
	DelayUntil time.Time `json:"delay_until"`
}

// AsRequestExecutionResult safely casts an AnyExecutionResult to a RequestExecutionResult.
func AsRequestExecutionResult(result AnyExecutionResult) (*RequestExecutionResult, bool) {
	reqResult, ok := result.(*RequestExecutionResult)
	return reqResult, ok
}

// MustAsRequestExecutionResult casts an AnyExecutionResult to a RequestExecutionResult, panicking if it fails.
func MustAsRequestExecutionResult(result AnyExecutionResult) *RequestExecutionResult {
	reqResult, ok := AsRequestExecutionResult(result)
	if !ok {
		panic("expected RequestExecutionResult but got different type")
	}
	return reqResult
}

// AsDelayExecutionResult safely casts an AnyExecutionResult to a DelayExecutionResult.
func AsDelayExecutionResult(result AnyExecutionResult) (*DelayExecutionResult, bool) {
	delayResult, ok := result.(*DelayExecutionResult)
	return delayResult, ok
}

// MustAsDelayExecutionResult casts an AnyExecutionResult to a DelayExecutionResult, panicking if it fails.
func MustAsDelayExecutionResult(result AnyExecutionResult) *DelayExecutionResult {
	delayResult, ok := AsDelayExecutionResult(result)
	if !ok {
		panic("expected DelayExecutionResult but got different type")
	}
	return delayResult
}

// FlowExecutionResult contains the complete trace of a flow execution.
type FlowExecutionResult struct {
	ExecutionResults map[string]AnyExecutionResult `json:"execution_results"` // Polymorphic results!
	FinalOutputs     map[string]interface{}        `json:"final_outputs"`     // All outputs flattened for convenience (format: "nodeId.outputKey": value)
	Success          bool                          `json:"success"`
	Error            error                         `json:"-"`
	ErrorCode        *string                       `json:"error_code,omitempty"`
	ErrorMsg         *string                       `json:"error_message,omitempty"`
	DurationMS       int64                         `json:"duration_ms"`
}
