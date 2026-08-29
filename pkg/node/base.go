package node

import (
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Output represents a named output with an associated extractor.
type Output struct {
	Name      string                  `json:"name"`
	Extractor extractors.AnyExtractor `json:"extractor"`
}

// BaseNode contains common fields and behavior shared across all node types.
// All specific node types (RequestNode, DelayNode, AssertionNode, etc.) should embed BaseNode.
type BaseNode struct {
	ID          string               `json:"id"`
	DisplayName string               `json:"display_name"`
	NodeType    spi.Kind             `json:"type"`
	RunWhen     spi.RunWhen          `json:"run_when,omitempty"`
	Assertions  []CompositeAssertion `json:"assertions"`
	Outputs     []Output             `json:"outputs"`
}

// GetID returns the unique identifier for this node.
func (bn *BaseNode) GetID() string {
	return bn.ID
}

// GetDisplayName returns the display name for this node.
func (bn *BaseNode) GetDisplayName() string {
	return bn.DisplayName
}

// GetType returns the type of this node (request, delay, assertion, etc.)
func (bn *BaseNode) GetType() spi.Kind {
	return bn.NodeType
}

func (bn *BaseNode) GetRunWhen() spi.RunWhen {
	if bn.RunWhen == "" {
		return spi.RunWhenOnSuccess
	}
	return bn.RunWhen
}

// InputSchema returns the list of required inputs for this node
// This method must be overridden by concrete node types to provide computed schemas
// Format: "nodeId.outputKey" (e.g., "create-user.userId") or plain variable name.
func (bn *BaseNode) InputSchema() []string {
	// Default implementation - should be overridden by concrete types
	return []string{}
}

// OutputSchema returns the list of outputs this node produces
// This method must be overridden by concrete node types to provide computed schemas
// Examples: []string{"statusCode", "userId", "responseBody"}.
func (bn *BaseNode) OutputSchema() []string {
	// Default implementation - should be overridden by concrete types
	return []string{}
}

// GetAssertions returns the list of assertions to validate during execution
// Assertions should be evaluated before extractions.
func (bn *BaseNode) GetAssertions() []CompositeAssertion {
	return bn.Assertions
}

// GetOutputs returns the list of extractions to perform on the response/data
// Outputs should be evaluated after assertions pass.
func (bn *BaseNode) GetOutputs() []Output {
	return bn.Outputs
}

// baseResult builds the envelope every node result carries: which node ran, of
// what kind, over which inputs, producing which outputs, and when. Concrete node
// types embed the returned value and add only their own fields.
//
// kind is passed rather than read from bn.NodeType because the field is
// populated from the wire and a node built in code may leave it empty; each node
// type knows its own kind statically.
func (bn *BaseNode) baseResult(kind spi.Kind, inputs, outputs map[string]any) spi.BaseExecutionResult {
	return spi.BaseExecutionResult{
		NodeID:      bn.GetID(),
		DisplayName: bn.GetDisplayName(),
		NodeType:    kind,
		Inputs:      inputs,
		Outputs:     outputs,
		ExecutedAt:  time.Now(),
	}
}

// failedBaseResult is baseResult for a node that failed: no outputs, plus the
// failure recorded the three ways the result contract wants it — the live Go
// error for errors.Is/As, and the message/code pointers echopoint reads as
// error_message/error_code.
//
// It deliberately does NOT classify: the message is always err.Error() and the
// code is always the one passed in. That preserves each node kind's published
// contract — the SSE node, for one, documents that its code stays SSE_FAILED
// even when the failure is a spi.UserError. Reach for
// spi.BaseExecutionResult.Fail instead where a UserError's own message and code
// should win, as the request node and the engine's assertion pass do.
func (bn *BaseNode) failedBaseResult(
	kind spi.Kind,
	inputs map[string]any,
	err error,
	code string,
) spi.BaseExecutionResult {
	message := err.Error()

	result := bn.baseResult(kind, inputs, nil)
	result.Error = err
	result.ErrorMsg = &message
	result.ErrorCode = &code
	return result
}
