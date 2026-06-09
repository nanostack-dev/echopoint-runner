package spi

import "context"

// OutputView is a read-only snapshot of outputs from already-completed nodes.
type OutputView interface {
	HasNode(nodeID string) bool
	Get(nodeID, outputKey string) (any, bool)
	// Node returns a defensive copy of the requested node outputs.
	Node(nodeID string) map[string]any
}

// DynamicResolver resolves a {{$name:args}} dynamic template variable to a
// generated value. Implemented by pkg/dynamicvars.
type DynamicResolver interface {
	Resolve(name string, args []string) (string, error)
}

// ResolvedModuleFlow is a flow definition (plus input overrides) made available
// to a module node for nested execution.
type ResolvedModuleFlow struct {
	FlowDefinition []byte
	InputOverrides map[string]any
}

// ModuleResolver exposes the additional flow definitions available to module
// nodes during nested execution.
type ModuleResolver interface {
	ResolveFlow(flowID string) (ResolvedModuleFlow, bool)
}

// ModuleExecutionRequest is a request to run a nested flow for a module node.
type ModuleExecutionRequest struct {
	FlowID         string
	FlowDefinition []byte
	Inputs         map[string]any
}

// ModuleExecutor runs nested flows for module nodes.
type ModuleExecutor interface {
	ExecuteModule(request ModuleExecutionRequest) (*FlowExecutionResult, error)
}

// ExecutionContext provides inputs and context for a node's execution.
type ExecutionContext struct {
	// Ctx is the request-scoped context for the execution. Nodes use it for
	// cancellation and deadlines (e.g. the HTTP request honors it). May be nil,
	// in which case callers should treat it as context.Background().
	Ctx context.Context
	// Inputs contains all the data this node declared it needs in InputSchema().
	// Keys are in format "nodeId.outputKey" (e.g., "create-user.userId").
	Inputs map[string]any
	// FlowInputs contains the full effective inputs for the current flow execution,
	// including inherited inputs, static overrides, and any initial input values.
	FlowInputs map[string]any
	// AllOutputs exposes a read-only snapshot of outputs from nodes that completed
	// before the current scheduling batch started.
	AllOutputs OutputView
	// ModuleResolver exposes the additional flow definitions available to module
	// nodes during nested execution.
	ModuleResolver ModuleResolver
	// ModuleExecutor runs nested flows for module nodes.
	ModuleExecutor ModuleExecutor
	// DynamicVars resolves {{$name}} template variables (fake-data generators).
	// May be nil, in which case {{$...}} references are left untouched.
	DynamicVars DynamicResolver
}

// Context returns the execution context, defaulting to context.Background() when
// none was provided so callers never need a nil check.
func (c ExecutionContext) Context() context.Context {
	if c.Ctx != nil {
		return c.Ctx
	}
	return context.Background()
}

// Node is the engine's core view of any flow node — the capability-agnostic
// surface the scheduler drives. The full authoring interface (node.AnyNode)
// embeds this and adds the assertion/output accessors, which carry concrete
// extractor decode/eval behavior and so live in pkg/node.
type Node interface {
	GetID() string
	GetDisplayName() string
	GetType() Kind
	GetRunWhen() RunWhen
	InputSchema() []string

	// OutputSchema defines what this node produces
	// Examples: []string{"statusCode", "userId", "responseBody"}
	OutputSchema() []string

	// Execute performs the node's action with provided inputs and returns the
	// polymorphic result. Error indicates execution failure.
	Execute(ctx ExecutionContext) (AnyResult, error)
}
