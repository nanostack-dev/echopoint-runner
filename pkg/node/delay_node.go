package node

import (
	"fmt"
	"time"
)

type DelayData struct {
	Duration int `json:"duration"` // Duration in milliseconds
}

// DelayNode is a typed node for delays.
type DelayNode struct {
	BaseNode

	Data DelayData `json:"data"`
}

// AsDelayNode safely casts an AnyNode to a DelayNode
// Returns the DelayNode and true if the cast succeeds, nil and false otherwise.
func AsDelayNode(node AnyNode) (*DelayNode, bool) {
	delayNode, ok := node.(*DelayNode)
	return delayNode, ok
}

// MustAsDelayNode casts an AnyNode to a DelayNode, panicking if it fails
// Use this when you're certain the node is a DelayNode.
func MustAsDelayNode(node AnyNode) *DelayNode {
	delayNode, ok := AsDelayNode(node)
	if !ok {
		panic("expected DelayNode but got different type")
	}
	return delayNode
}

// InputSchema returns empty as DelayNode doesn't need inputs.
func (n *DelayNode) InputSchema() []string {
	return []string{}
}

// OutputSchema returns empty as DelayNode doesn't produce outputs.
func (n *DelayNode) OutputSchema() []string {
	return []string{}
}

// Execute sleeps for the specified duration and optionally passes through input values.
func (n *DelayNode) Execute(ctx ExecutionContext) (map[string]interface{}, error) {
	// Validate that we have all required inputs
	for _, dep := range n.InputSchema() {
		if _, exists := ctx.Inputs[dep]; !exists {
			return nil, fmt.Errorf("missing required input: %s", dep)
		}
	}

	// Sleep for the specified duration
	time.Sleep(time.Duration(n.Data.Duration) * time.Millisecond)

	// DelayNode typically passes through inputs as outputs (or returns empty if no outputs declared)
	output := make(map[string]interface{})

	// If no specific outputs are declared, return empty map
	// If outputs are declared, copy matching inputs to outputs
	for _, outputKey := range n.OutputSchema() {
		if val, exists := ctx.Inputs[outputKey]; exists {
			output[outputKey] = val
		}
	}

	return output, nil
}

func (n *DelayNode) GetData() DelayData {
	return n.Data
}
