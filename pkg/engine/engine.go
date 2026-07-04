package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type Options struct {
	Observer        ExecutionObserver
	ModuleResolver  spi.ModuleResolver
	ModuleCallStack []string
	DynamicVars     spi.DynamicResolver
	// Ctx is the request-scoped context propagated to every node execution for
	// cancellation/deadlines. Nil is treated as context.Background().
	Ctx context.Context
	// Middleware wraps each node's execution (retry/timeout/tracing). Outermost first.
	Middleware []Middleware
}

type FlowEngine struct {
	flow            flow.Flow
	nodeEdgeOutput  map[node.AnyNode][]node.AnyNode
	nodeEdgeSource  map[node.AnyNode][]node.AnyNode
	nodeEdgeInput   map[node.AnyNode]int
	nodeMap         map[string]node.AnyNode
	observer        ExecutionObserver
	moduleResolver  spi.ModuleResolver
	moduleCallStack []string
	dynamicVars     spi.DynamicResolver
	ctx             context.Context
	middleware      []Middleware
}

type moduleExecutor struct {
	resolver  spi.ModuleResolver
	callStack []string
	ctx       context.Context
}

// ExecuteModule runs a nested module flow. Every failure it returns is caused by
// the referenced flow's definition (missing flow_id, a module cycle, or a flow
// that fails to parse/validate) rather than a fault in the runner, so each is
// returned as a spi.UserError. This keeps the node executor logging them at
// debug — the same way it treats input-validation failures — instead of error,
// which would otherwise inflate error metrics and trip error-rate alerts on a
// user's invalid flow definition.
func (e moduleExecutor) ExecuteModule(request spi.ModuleExecutionRequest) (*spi.FlowExecutionResult, error) {
	trimmedFlowID := strings.TrimSpace(request.FlowID)
	if trimmedFlowID == "" {
		return nil, spi.NewUserError("MODULE_FLOW_ID_REQUIRED", "module flow_id is required", nil)
	}
	if slices.Contains(e.callStack, trimmedFlowID) {
		cycle := append(append([]string{}, e.callStack...), trimmedFlowID)
		return nil, spi.NewUserError(
			"MODULE_CYCLE_DETECTED",
			fmt.Sprintf("module cycle detected: %s", strings.Join(cycle, " -> ")),
			nil,
		)
	}

	parsedFlow, err := flow.ParseFromJSONWithOptions(request.FlowDefinition, flow.ParseOptions{
		AllowedInitialInputKeys: sortedInputKeys(request.Inputs),
	})
	if err != nil {
		return nil, spi.NewUserError("MODULE_FLOW_PARSE_FAILED", "parse module flow", err)
	}

	return ExecuteFlowDefinition(*parsedFlow, request.Inputs, &Options{
		ModuleResolver:  e.resolver,
		ModuleCallStack: append(append([]string{}, e.callStack...), trimmedFlowID),
		Ctx:             e.ctx,
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
		flow:            flowInstance,
		nodeEdgeOutput:  nodeEdgeOutput,
		nodeEdgeSource:  nodeEdgeSource,
		nodeEdgeInput:   nodeEdgeInput,
		nodeMap:         nodeMap,
		observer:        observer,
		moduleResolver:  nilIfNoModuleResolverFromOptions(options),
		moduleCallStack: cloneStringSlice(moduleCallStackFromOptions(options)),
		dynamicVars:     dynamicVarsFromOptions(options),
		ctx:             ctxFromOptions(options),
		middleware:      middlewareFromOptions(options),
	}, nil
}

// middlewareFromOptions returns the middleware chain from engine options, or nil.
func middlewareFromOptions(options *Options) []Middleware {
	if options == nil {
		return nil
	}
	return options.Middleware
}

// ctxFromOptions returns the context from engine options, defaulting to
// context.Background() when unset.
func ctxFromOptions(options *Options) context.Context {
	if options != nil && options.Ctx != nil {
		return options.Ctx
	}
	return context.Background()
}

// dynamicVarsFromOptions returns the dynamic-variable resolver from engine
// options, or nil.
func dynamicVarsFromOptions(options *Options) spi.DynamicResolver {
	if options == nil {
		return nil
	}
	return options.DynamicVars
}

func (engine *FlowEngine) Execute(initialInputs map[string]any) (
	*spi.FlowExecutionResult, error,
) {
	return ExecuteFlowDefinition(engine.flow, initialInputs, &Options{
		Observer:        engine.observer,
		ModuleResolver:  engine.moduleResolver,
		ModuleCallStack: cloneStringSlice(engine.moduleCallStack),
		DynamicVars:     engine.dynamicVars,
		Ctx:             engine.ctx,
		Middleware:      engine.middleware,
	})
}

func ExecuteFlowDefinition(
	flowInstance flow.Flow,
	initialInputs map[string]any,
	options *Options,
) (*spi.FlowExecutionResult, error) {
	startTime := time.Now()
	result := &spi.FlowExecutionResult{
		ExecutionResults: make(map[string]spi.AnyResult),
		FinalOutputs:     make(map[string]any),
		Success:          false,
	}

	if validateErr := validateModuleGraph(
		flowInstance,
		nilIfNoModuleResolverFromOptions(options),
		moduleCallStackFromOptions(options),
	); validateErr != nil {
		errMsg := validateErr.Error()
		errCode := "FLOW_VALIDATION_FAILED"
		result.Error = validateErr
		result.ErrorMsg = &errMsg
		result.ErrorCode = &errCode
		result.DurationMS = time.Since(startTime).Milliseconds()
		return result, validateErr
	}

	flowEngine, err := NewFlowEngine(flowInstance, options)
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

func nilIfNoModuleResolverFromOptions(options *Options) spi.ModuleResolver {
	if options == nil {
		return nil
	}
	return options.ModuleResolver
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

func sortedInputKeys(inputs map[string]any) []string {
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
	nodeToExecute node.AnyNode, allOutputs spi.OutputView,
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
	nodeToExecute node.AnyNode, allOutputs spi.OutputView,
) map[string]any {
	inputs := make(map[string]any)

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
