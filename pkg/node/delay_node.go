package node

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
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

// Execute sleeps for the specified duration and returns a DelayExecutionResult.
func (n *DelayNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()
	delayMs := n.Data.Duration

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("durationMS", delayMs).
		Msg("Starting delay node execution")

	// Validate that we have all required inputs
	for _, dep := range n.InputSchema() {
		if _, exists := ctx.Inputs[dep]; !exists {
			err := fmt.Errorf("missing required input: %s", dep)
			log.Error().
				Str("nodeID", n.GetID()).
				Str("missingInput", dep).
				Err(err).
				Msg("Delay node input validation failed")
			return nil, err
		}
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("durationMS", delayMs).
		Msg("Starting delay")

	// Sleep for the specified duration
	time.Sleep(time.Duration(delayMs) * time.Millisecond)

	// DelayNode typically doesn't produce outputs, but may pass through declared outputs
	outputs := make(map[string]interface{})
	for _, outputKey := range n.OutputSchema() {
		if val, exists := ctx.Inputs[outputKey]; exists {
			outputs[outputKey] = val
		}
	}

	result := &DelayExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeDelay,
			Inputs:      ctx.Inputs,
			Outputs:     outputs,
			ExecutedAt:  time.Now(),
		},
		DelayMs:    int64(delayMs),
		DelayUntil: startTime.Add(time.Duration(delayMs) * time.Millisecond),
	}

	log.Info().
		Str("nodeID", n.GetID()).
		Int64("delayMs", result.DelayMs).
		Msg("Delay node executed successfully")

	return result, nil
}

func (n *DelayNode) GetData() DelayData {
	return n.Data
}
