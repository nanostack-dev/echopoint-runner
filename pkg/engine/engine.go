package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/flow"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
)

type Options struct {
	BeforeExecution func(n node.AnyNode)
	AfterExecution  func(n node.AnyNode, frame node.ExecutionResult)
}

type FlowEngine struct {
	flow            flow.Flow
	nodeEdgeOutput  map[node.AnyNode][]node.AnyNode
	nodeEdgeInput   map[node.AnyNode]int
	nodeMap         map[string]node.AnyNode
	beforeExecution func(n node.AnyNode)
	afterExecution  func(n node.AnyNode, frame node.ExecutionResult)
}

func NewFlowEngine(flowInstance flow.Flow, options *Options) (*FlowEngine, error) {
	nodeMap := make(map[string]node.AnyNode, len(flowInstance.Nodes))
	nodeEdgeOutput := make(map[node.AnyNode][]node.AnyNode)
	nodeEdgeInput := make(map[node.AnyNode]int)
	for _, nodeInstance := range flowInstance.Nodes {
		nodeMap[nodeInstance.GetID()] = nodeInstance
		nodeEdgeInput[nodeInstance] = 0
		nodeEdgeOutput[nodeInstance] = nil
	}
	for _, edge := range flowInstance.Edges {
		sourceNode := nodeMap[edge.Source]
		targetNode := nodeMap[edge.Target]
		if sourceNode == nil {
			return nil, fmt.Errorf(
				"source node %s not found in edge to node %s", edge.Source,
				edge.Target,
			)
		}
		if targetNode == nil {
			return nil, fmt.Errorf(
				"target node %s not found in edge to node %s", edge.Target,
				edge.Source,
			)
		}
		nodeEdgeOutput[sourceNode] = append(nodeEdgeOutput[sourceNode], targetNode)
		nodeEdgeInput[targetNode]++
	}
	var beforeExecution func(n node.AnyNode)
	var afterExecution func(n node.AnyNode, frame node.ExecutionResult)
	if options != nil {
		if options.BeforeExecution != nil {
			beforeExecution = options.BeforeExecution
		}
		if options.AfterExecution != nil {
			afterExecution = options.AfterExecution
		}
	}
	return &FlowEngine{
		flowInstance,
		nodeEdgeOutput,
		nodeEdgeInput,
		nodeMap,
		beforeExecution,
		afterExecution,
	}, nil
}

// Execute executes the flow with the provided initial inputs and returns complete execution results
func (engine *FlowEngine) Execute(initialInputs map[string]interface{}) (
	*node.FlowExecutionResult, error,
) {
	startTime := time.Now()
	result := &node.FlowExecutionResult{
		ExecutionResults: make(map[string]node.ExecutionResult),
		FinalOutputs:     make(map[string]interface{}),
		Success:          false,
	}

	if len(engine.nodeEdgeInput) == 0 {
		result.Error = errors.New("no nodes to execute")
		result.DurationMS = time.Since(startTime).Milliseconds()
		return result, result.Error
	}

	// Initialize AllOutputs with initial inputs directly as a flat map
	// Initial inputs don't have a node ID prefix
	allOutputs := make(map[string]map[string]interface{})
	allOutputs[""] = initialInputs

	// Create a copy of nodeEdgeInput for tracking execution order
	remainingInputs := make(map[node.AnyNode]int)
	for k, v := range engine.nodeEdgeInput {
		remainingInputs[k] = v
	}

	for {
		nodeToExecute := engine.findNodeWithoutInput(remainingInputs)
		if nodeToExecute == nil {
			if len(remainingInputs) > 0 {
				result.Error = fmt.Errorf(
					"cycle detected or unreachable nodes: %d nodes not executed",
					len(remainingInputs),
				)
				result.DurationMS = time.Since(startTime).Milliseconds()
				return result, result.Error
			}
			// All nodes executed successfully
			result.Success = true
			result.DurationMS = time.Since(startTime).Milliseconds()
			return result, nil
		}

		// Validate that all required inputs are available
		if err := engine.validateInputs(nodeToExecute, allOutputs); err != nil {
			result.Error = err
			result.DurationMS = time.Since(startTime).Milliseconds()
			return result, err
		}

		// Assemble inputs for this node from previous outputs
		inputs := engine.assembleInputs(nodeToExecute, allOutputs)

		// Execute the node
		if engine.beforeExecution != nil {
			engine.beforeExecution(nodeToExecute)
		}

		executionCtx := node.ExecutionContext{
			Inputs:     inputs,
			AllOutputs: allOutputs,
		}

		outputData, err := nodeToExecute.Execute(executionCtx)

		// Record the execution executionFrame
		executionFrame := node.ExecutionResult{
			NodeID:     nodeToExecute.GetID(),
			Inputs:     inputs,
			Outputs:    outputData,
			Error:      err,
			ExecutedAt: time.Now(),
		}
		result.ExecutionResults[nodeToExecute.GetID()] = executionFrame

		if engine.afterExecution != nil {
			engine.afterExecution(nodeToExecute, executionFrame)
		}

		// Handle execution failure
		if err != nil {
			result.Error = err
			result.DurationMS = time.Since(startTime).Milliseconds()
			return result, err
		}

		// Store outputs for next nodes
		allOutputs[nodeToExecute.GetID()] = outputData

		// Merge into final outputs (format: "nodeId.outputKey": value)
		for key, value := range outputData {
			flatKey := fmt.Sprintf("%s.%s", nodeToExecute.GetID(), key)
			result.FinalOutputs[flatKey] = value
		}

		// Reduce in-degrees for successor nodes
		nodeOutput := engine.nodeEdgeOutput[nodeToExecute]
		for _, nodeToReduce := range nodeOutput {
			remainingInputs[nodeToReduce]--
		}
		delete(remainingInputs, nodeToExecute)
	}
}

// validateInputs checks that all required inputs for a node are available in allOutputs
func (engine *FlowEngine) validateInputs(
	nodeToExecute node.AnyNode, allOutputs map[string]map[string]interface{},
) error {
	for _, inputKey := range nodeToExecute.InputSchema() {
		sourceNodeID, outputKey, err := parseDataRef(inputKey)
		if err != nil {
			return fmt.Errorf(
				"node %s: invalid input reference '%s': %w", nodeToExecute.GetID(), inputKey, err,
			)
		}

		sourceOutputs, exists := allOutputs[sourceNodeID]
		if !exists {
			return fmt.Errorf(
				"node %s: source node '%s' not executed yet (required for input '%s')",
				nodeToExecute.GetID(), sourceNodeID, inputKey,
			)
		}

		_, exists = sourceOutputs[outputKey]
		if !exists {
			return fmt.Errorf(
				"node %s: output '%s' not found in source node '%s'",
				nodeToExecute.GetID(), outputKey, sourceNodeID,
			)
		}
	}
	return nil
}

// assembleInputs gathers inputs for a node from previous outputs
func (engine *FlowEngine) assembleInputs(
	nodeToExecute node.AnyNode, allOutputs map[string]map[string]interface{},
) map[string]interface{} {
	inputs := make(map[string]interface{})

	for _, inputKey := range nodeToExecute.InputSchema() {
		sourceNodeID, outputKey, _ := parseDataRef(inputKey)
		sourceOutputs := allOutputs[sourceNodeID]
		value := sourceOutputs[outputKey]
		// Store with full reference key (e.g., "create-user.userId")
		inputs[inputKey] = value
	}

	return inputs
}

// parseDataRef parses input references in two formats:
// 1. "nodeId.outputKey" - refers to output from a specific node
// 2. "variableName" - refers to initial input variable (sourceNodeID will be empty string "")
func parseDataRef(ref string) (sourceNodeID, outputKey string, err error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) == 2 {
		// Format: "nodeId.outputKey"
		return parts[0], parts[1], nil
	}
	if len(parts) == 1 {
		// Format: "variableName" - initial input
		return "", parts[0], nil
	}
	return "", "", fmt.Errorf(
		"invalid reference format, expected 'nodeId.outputKey' or 'variableName', got '%s'", ref,
	)
}

// findNodeWithoutInput finds a node that has no remaining input dependencies
func (engine *FlowEngine) findNodeWithoutInput(remainingInputs map[node.AnyNode]int) node.AnyNode {
	for nodeKey, inputCount := range remainingInputs {
		if inputCount == 0 {
			return nodeKey
		}
	}
	return nil
}

// DEPRECATED: Use Execute(initialInputs) instead
func (engine *FlowEngine) ExecuteLegacy() error {
	var nodeToExecute node.AnyNode
	if len(engine.nodeEdgeInput) == 0 {
		return errors.New("no nodes to execute")
	}
	for {
		nodeToExecute = engine.findNodeWithoutInput(engine.nodeEdgeInput)
		if nodeToExecute == nil {
			if len(engine.nodeEdgeInput) > 0 {
				return fmt.Errorf(
					"cycle detected or unreachable nodes: %d nodes not executed",
					len(engine.nodeEdgeInput),
				)
			}
			return nil
		}
		if engine.beforeExecution != nil {
			engine.beforeExecution(nodeToExecute)
		}

		// This will fail because Execute now requires ExecutionContext
		// This is kept for reference only
		_, err := nodeToExecute.Execute(node.ExecutionContext{})
		if engine.afterExecution != nil {
			// Create empty frame for legacy
			engine.afterExecution(nodeToExecute, node.ExecutionResult{})
		}
		if err != nil {
			return fmt.Errorf("nodeToExecute failed with %w", err)
		}
		nodeOutput := engine.nodeEdgeOutput[nodeToExecute]
		for _, nodeToReduce := range nodeOutput {
			engine.nodeEdgeInput[nodeToReduce]--
		}
		delete(engine.nodeEdgeInput, nodeToExecute)
	}
}
