package node

import "time"

type AnyNode interface {
	GetID() string
	GetType() Type
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
	// Returns a map of output data keyed by names in OutputSchema()
	// Error indicates execution failure
	Execute(ctx ExecutionContext) (map[string]interface{}, error)
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

// ExecutionContext provides inputs and context for a node's execution.
type ExecutionContext struct {
	// Inputs contains all the data this node declared it needs in InputSchema()
	// Keys are in format "nodeId.outputKey" (e.g., "create-user.userId")
	Inputs map[string]interface{}
	// AllOutputs contains outputs from ALL nodes executed so far
	// Structure: map[nodeID]map[outputKey]value
	// (for advanced use cases like conditional data passing)
	AllOutputs map[string]map[string]interface{}
}

// ExecutionResult stores per-node execution results and metadata.
type ExecutionResult struct {
	NodeID     string
	Inputs     map[string]interface{}
	Outputs    map[string]interface{}
	Error      error
	ExecutedAt time.Time
}

// FlowExecutionResult contains the complete trace of a flow execution.
type FlowExecutionResult struct {
	ExecutionResults map[string]ExecutionResult // Execution frame keyed by node ID
	FinalOutputs     map[string]interface{}     // All outputs flattened for convenience (format: "nodeId.outputKey": value)
	Success          bool
	Error            error
	DurationMS       int64
}
