package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

type Options struct {
	Observer        ExecutionObserver
	ModuleResolver  node.ModuleResolver
	ModuleCallStack []string
}

type FlowEngine struct {
	flow            flow.Flow
	nodeEdgeOutput  map[node.AnyNode][]node.AnyNode
	nodeEdgeSource  map[node.AnyNode][]node.AnyNode
	nodeEdgeInput   map[node.AnyNode]int
	nodeMap         map[string]node.AnyNode
	observer        ExecutionObserver
	moduleResolver  node.ModuleResolver
	moduleCallStack []string
}

type moduleExecutor struct {
	resolver  node.ModuleResolver
	callStack []string
}

func (e moduleExecutor) ExecuteModule(request node.ModuleExecutionRequest) (*node.FlowExecutionResult, error) {
	trimmedFlowID := strings.TrimSpace(request.FlowID)
	if trimmedFlowID == "" {
		return nil, errors.New("module flow_id is required")
	}
	for _, activeFlowID := range e.callStack {
		if activeFlowID == trimmedFlowID {
			cycle := append(append([]string{}, e.callStack...), trimmedFlowID)
			return nil, fmt.Errorf("module cycle detected: %s", strings.Join(cycle, " -> "))
		}
	}

	parsedFlow, err := flow.ParseFromJSONWithOptions(request.FlowDefinition, flow.ParseOptions{
		AllowedInitialInputKeys: sortedInputKeys(request.Inputs),
	})
	if err != nil {
		return nil, fmt.Errorf("parse module flow: %w", err)
	}

	return ExecuteFlowDefinition(*parsedFlow, request.Inputs, &ExecuteOptions{
		ModuleResolver:  e.resolver,
		ModuleCallStack: append(append([]string{}, e.callStack...), trimmedFlowID),
	})
}

func NewFlowEngine(flowInstance flow.Flow, options *Options) (*FlowEngine, error) {
	nodeMap := make(map[string]node.AnyNode, len(flowInstance.Nodes))
	nodeEdgeOutput := make(map[node.AnyNode][]node.AnyNode)
	nodeEdgeSource := make(map[node.AnyNode][]node.AnyNode)
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
		nodeEdgeSource[nodeInstance] = nil
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
		nodeEdgeSource[targetNode] = append(nodeEdgeSource[targetNode], sourceNode)
		nodeEdgeInput[targetNode]++
		log.Debug().
			Str("flowName", flowInstance.Name).
			Str("edgeID", edge.ID).
			Str("sourceNodeID", edge.Source).
			Str("targetNodeID", edge.Target).
			Str("edgeType", string(edge.Type)).
			Msg("Registered edge")
	}

	observer := ExecutionObserver(NoopObserver{})
	if options != nil && options.Observer != nil {
		observer = ensureSynchronizedObserver(options.Observer)
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
		nodeEdgeSource,
		nodeEdgeInput,
		nodeMap,
		observer,
		nilIfNoModuleResolverFromOptions(options),
		cloneStringSlice(moduleCallStackFromOptions(options)),
	}, nil
}

func (engine *FlowEngine) Execute(initialInputs map[string]interface{}) (
	*node.FlowExecutionResult, error,
) {
	return ExecuteFlowDefinition(engine.flow, initialInputs, &ExecuteOptions{
		Observer:        engine.observer,
		ModuleResolver:  engine.moduleResolver,
		ModuleCallStack: cloneStringSlice(engine.moduleCallStack),
	})
}

type ExecuteOptions struct {
	Observer        ExecutionObserver
	ModuleResolver  node.ModuleResolver
	ModuleCallStack []string
}

func ExecuteFlowDefinition(
	flowInstance flow.Flow,
	initialInputs map[string]interface{},
	options *ExecuteOptions,
) (*node.FlowExecutionResult, error) {
	startTime := time.Now()
	result := &node.FlowExecutionResult{
		ExecutionResults: make(map[string]node.AnyExecutionResult),
		FinalOutputs:     make(map[string]interface{}),
		Success:          false,
	}

	if validateErr := validateModuleGraph(
		flowInstance,
		nilIfNoModuleResolver(options),
		moduleCallStack(options),
	); validateErr != nil {
		errMsg := validateErr.Error()
		errCode := "FLOW_VALIDATION_FAILED"
		result.Error = validateErr
		result.ErrorMsg = &errMsg
		result.ErrorCode = &errCode
		result.DurationMS = time.Since(startTime).Milliseconds()
		return result, validateErr
	}

	observer := ExecutionObserver(NoopObserver{})
	if options != nil && options.Observer != nil {
		observer = options.Observer
	}
	flowEngine, err := NewFlowEngine(flowInstance, &Options{
		Observer:        observer,
		ModuleResolver:  nilIfNoModuleResolver(options),
		ModuleCallStack: moduleCallStack(options),
	})
	if err != nil {
		return nil, err
	}

	log.Info().
		Str("flowName", flowEngine.flow.Name).
		Str("flowVersion", flowEngine.flow.Version).
		Int("totalNodes", len(flowEngine.flow.Nodes)).
		Int("totalEdges", len(flowEngine.flow.Edges)).
		Msg("Starting flow execution")

	flowEngine.observer.FlowStarted(FlowStartedEvent{
		FlowName:  flowEngine.flow.Name,
		StartedAt: startTime,
	})

	if len(flowEngine.nodeEdgeInput) == 0 {
		result.Error = errors.New("no nodes to execute")
		result.DurationMS = time.Since(startTime).Milliseconds()
		flowEngine.observer.FlowFinished(FlowFinishedEvent{
			FlowName:   flowEngine.flow.Name,
			StartedAt:  startTime,
			FinishedAt: time.Now(),
			DurationMs: result.DurationMS,
			Result:     result,
		})
		log.Error().
			Str("flowName", flowEngine.flow.Name).
			Err(result.Error).
			Int64("durationMS", result.DurationMS).
			Msg("Flow execution failed: no nodes to execute")
		return result, result.Error
	}

	execErr := flowEngine.executeNodes(initialInputs, result, startTime)
	if execErr != nil {
		flowEngine.observer.FlowFinished(FlowFinishedEvent{
			FlowName:   flowEngine.flow.Name,
			StartedAt:  startTime,
			FinishedAt: time.Now(),
			DurationMs: result.DurationMS,
			Result:     result,
		})
		return result, execErr
	}
	flowEngine.observer.FlowFinished(FlowFinishedEvent{
		FlowName:   flowEngine.flow.Name,
		StartedAt:  startTime,
		FinishedAt: time.Now(),
		DurationMs: result.DurationMS,
		Result:     result,
	})

	return result, nil
}

func nilIfNoModuleResolver(options *ExecuteOptions) node.ModuleResolver {
	if options == nil {
		return nil
	}
	return options.ModuleResolver
}

func nilIfNoModuleResolverFromOptions(options *Options) node.ModuleResolver {
	if options == nil {
		return nil
	}
	return options.ModuleResolver
}

func moduleCallStack(options *ExecuteOptions) []string {
	if options == nil {
		return nil
	}
	return cloneStringSlice(options.ModuleCallStack)
}

func moduleCallStackFromOptions(options *Options) []string {
	if options == nil {
		return nil
	}
	return options.ModuleCallStack
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func sortedInputKeys(inputs map[string]interface{}) []string {
	keys := make([]string, 0, len(inputs))
	for key := range inputs {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		keys = append(keys, trimmed)
	}
	sort.Strings(keys)
	return keys
}

// validateInputs checks that all required inputs for a node are available in allOutputs.
func (engine *FlowEngine) validateInputs(
	nodeToExecute node.AnyNode, allOutputs node.OutputView,
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

		if !allOutputs.HasNode(sourceNodeID) {
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

		_, exists := allOutputs.Get(sourceNodeID, outputKey)
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
	nodeToExecute node.AnyNode, allOutputs node.OutputView,
) map[string]interface{} {
	inputs := make(map[string]interface{})

	for _, inputKey := range nodeToExecute.InputSchema() {
		sourceNodeID, outputKey, _ := parseDataRef(inputKey)
		value, _ := allOutputs.Get(sourceNodeID, outputKey)
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
