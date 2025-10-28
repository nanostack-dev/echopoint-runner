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

// Execute sleeps for the specified duration and optionally passes through input values.
func (n *DelayNode) Execute(ctx ExecutionContext) (map[string]interface{}, error) {
	log.Debug().
		Str("nodeID", n.GetID()).
		Int("durationMS", n.Data.Duration).
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
		Int("durationMS", n.Data.Duration).
		Msg("Starting delay")

	// Sleep for the specified duration
	startTime := time.Now()
	time.Sleep(time.Duration(n.Data.Duration) * time.Millisecond)
	actualDuration := time.Since(startTime)

	log.Debug().
		Str("nodeID", n.GetID()).
		Int64("actualDurationMS", actualDuration.Milliseconds()).
		Msg("Delay completed")

	// DelayNode typically passes through inputs as outputs (or returns empty if no outputs declared)
	output := make(map[string]interface{})

	// If no specific outputs are declared, return empty map
	// If outputs are declared, copy matching inputs to outputs
	for _, outputKey := range n.OutputSchema() {
		if val, exists := ctx.Inputs[outputKey]; exists {
			output[outputKey] = val
		}
	}

	log.Info().
		Str("nodeID", n.GetID()).
		Int64("durationMS", actualDuration.Milliseconds()).
		Msg("Delay node executed successfully")

	return output, nil
}

func (n *DelayNode) GetData() DelayData {
	return n.Data
}
