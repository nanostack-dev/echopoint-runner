package node

import "github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"

// Output represents a named output with an associated extractor
type Output struct {
	Name      string                  `json:"name"`
	Extractor extractors.AnyExtractor `json:"extractor"`
}

// BaseNode contains common fields and behavior shared across all node types.
// All specific node types (RequestNode, DelayNode, AssertionNode, etc.) should embed BaseNode.
type BaseNode struct {
	ID         string               `json:"id"`
	NodeType   Type                 `json:"type"`
	Assertions []CompositeAssertion `json:"assertions"`
	Outputs    []Output             `json:"outputs"`
}

// GetID returns the unique identifier for this node
func (bn *BaseNode) GetID() string {
	return bn.ID
}

// GetType returns the type of this node (request, delay, assertion, etc.)
func (bn *BaseNode) GetType() Type {
	return bn.NodeType
}

// InputSchema returns the list of required inputs for this node
// This method must be overridden by concrete node types to provide computed schemas
// Format: "nodeId.outputKey" (e.g., "create-user.userId") or plain variable name
func (bn *BaseNode) InputSchema() []string {
	// Default implementation - should be overridden by concrete types
	return []string{}
}

// OutputSchema returns the list of outputs this node produces
// This method must be overridden by concrete node types to provide computed schemas
// Examples: []string{"statusCode", "userId", "responseBody"}
func (bn *BaseNode) OutputSchema() []string {
	// Default implementation - should be overridden by concrete types
	return []string{}
}

// GetAssertions returns the list of assertions to validate during execution
// Assertions should be evaluated before extractions
func (bn *BaseNode) GetAssertions() []CompositeAssertion {
	return bn.Assertions
}

// GetOutputs returns the list of extractions to perform on the response/data
// Outputs should be evaluated after assertions pass
func (bn *BaseNode) GetOutputs() []Output {
	return bn.Outputs
}
