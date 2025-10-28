package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

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

	log.Debug().
		Str("flowName", flowInstance.Name).
		Str("flowVersion", flowInstance.Version).
		Int("nodeCount", len(flowInstance.Nodes)).
		Int("edgeCount", len(flowInstance.Edges)).
		Msg("Initializing flow engine")

	for _, nodeInstance := range flowInstance.Nodes {
		nodeMap[nodeInstance.GetID()] = nodeInstance
		nodeEdgeInput[nodeInstance] = 0
		nodeEdgeOutput[nodeInstance] = nil
		log.Debug().
			Str("flowName", flowInstance.Name).
			Str("nodeID", nodeInstance.GetID()).
			Str("nodeType", string(nodeInstance.GetType())).
			Msg("Registered node")
	}

	for _, edge := range flowInstance.Edges {
		sourceNode := nodeMap[edge.Source]
		targetNode := nodeMap[edge.Target]
		if sourceNode == nil {
			err := fmt.Errorf(
				"source node %s not found in edge to node %s", edge.Source,
				edge.Target,
			)
			log.Error().
				Str("flowName", flowInstance.Name).
				Str("edgeID", edge.ID).
				Str("sourceNodeID", edge.Source).
				Str("targetNodeID", edge.Target).
				Err(err).
				Msg("Failed to initialize flow engine: source node not found")
			return nil, err
		}
		if targetNode == nil {
			err := fmt.Errorf(
				"target node %s not found in edge to node %s", edge.Target,
				edge.Source,
			)
			log.Error().
				Str("flowName", flowInstance.Name).
				Str("edgeID", edge.ID).
				Str("sourceNodeID", edge.Source).
				Str("targetNodeID", edge.Target).
				Err(err).
				Msg("Failed to initialize flow engine: target node not found")
			return nil, err
		}
		nodeEdgeOutput[sourceNode] = append(nodeEdgeOutput[sourceNode], targetNode)
		nodeEdgeInput[targetNode]++
		log.Debug().
			Str("flowName", flowInstance.Name).
			Str("edgeID", edge.ID).
			Str("sourceNodeID", edge.Source).
			Str("targetNodeID", edge.Target).
			Str("edgeType", string(edge.Type)).
			Msg("Registered edge")
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

	log.Info().
		Str("flowName", flowInstance.Name).
		Str("flowVersion", flowInstance.Version).
		Int("nodeCount", len(flowInstance.Nodes)).
		Int("edgeCount", len(flowInstance.Edges)).
		Msg("Flow engine initialized successfully")

	return &FlowEngine{
		flowInstance,
		nodeEdgeOutput,
		nodeEdgeInput,
		nodeMap,
		beforeExecution,
		afterExecution,
	}, nil
}

func (engine *FlowEngine) Execute(initialInputs map[string]interface{}) (
	*node.FlowExecutionResult, error,
) {
	startTime := time.Now()

	log.Info().
		Str("flowName", engine.flow.Name).
		Str("flowVersion", engine.flow.Version).
		Int("totalNodes", len(engine.flow.Nodes)).
		Int("totalEdges", len(engine.flow.Edges)).
		Msg("Starting flow execution")

	result := &node.FlowExecutionResult{
		ExecutionResults: make(map[string]node.ExecutionResult),
		FinalOutputs:     make(map[string]interface{}),
		Success:          false,
	}

	if len(engine.nodeEdgeInput) == 0 {
		result.Error = errors.New("no nodes to execute")
		result.DurationMS = time.Since(startTime).Milliseconds()
		log.Error().
			Str("flowName", engine.flow.Name).
			Err(result.Error).
			Int64("durationMS", result.DurationMS).
			Msg("Flow execution failed: no nodes to execute")
		return result, result.Error
	}

	if err := engine.executeNodes(initialInputs, result, startTime); err != nil {
		return result, err
	}

	return result, nil
}

// validateInputs checks that all required inputs for a node are available in allOutputs.
func (engine *FlowEngine) validateInputs(
	nodeToExecute node.AnyNode, allOutputs map[string]map[string]interface{},
) error {
	for _, inputKey := range nodeToExecute.InputSchema() {
		sourceNodeID, outputKey, err := parseDataRef(inputKey)
		if err != nil {
			log.Error().
				Str("flowName", engine.flow.Name).
				Str("nodeID", nodeToExecute.GetID()).
				Str("inputKey", inputKey).
				Err(err).
				Msg("Invalid input reference")
			return fmt.Errorf(
				"node %s: invalid input reference '%s': %w", nodeToExecute.GetID(), inputKey, err,
			)
		}

		sourceOutputs, exists := allOutputs[sourceNodeID]
		if !exists {
			log.Warn().
				Str("flowName", engine.flow.Name).
				Str("nodeID", nodeToExecute.GetID()).
				Str("sourceNodeID", sourceNodeID).
				Str("inputKey", inputKey).
				Msg("Source node not executed yet")
			return fmt.Errorf(
				"node %s: source node '%s' not executed yet (required for input '%s')",
				nodeToExecute.GetID(), sourceNodeID, inputKey,
			)
		}

		_, exists = sourceOutputs[outputKey]
		if !exists {
			log.Warn().
				Str("flowName", engine.flow.Name).
				Str("nodeID", nodeToExecute.GetID()).
				Str("sourceNodeID", sourceNodeID).
				Str("outputKey", outputKey).
				Msg("Output not found in source node")
			return fmt.Errorf(
				"node %s: output '%s' not found in source node '%s'",
				nodeToExecute.GetID(), outputKey, sourceNodeID,
			)
		}
	}
	return nil
}

// assembleInputs gathers inputs for a node from previous outputs.
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
// 2. "variableName" - refers to initial input variable (sourceNodeID will be empty string "").
func parseDataRef(ref string) (string, string, error) {
	const (
		refSeparator = "."
		partCount    = 2
	)
	parts := strings.SplitN(ref, refSeparator, partCount)
	if len(parts) == partCount {
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

// findNodeWithoutInput finds a node that has no remaining input dependencies.
func (engine *FlowEngine) findNodeWithoutInput(remainingInputs map[node.AnyNode]int) node.AnyNode {
	for nodeKey, inputCount := range remainingInputs {
		if inputCount == 0 {
			return nodeKey
		}
	}
	return nil
}

// ExecuteLegacy is deprecated. Use Execute(initialInputs) instead.
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
