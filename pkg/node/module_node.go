package node

import (
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/rs/zerolog/log"
)

type ModuleData struct {
	FlowID         string            `json:"flow_id"`
	InputBindings  map[string]any    `json:"input_bindings,omitempty"`
	OutputBindings map[string]string `json:"output_bindings,omitempty"`
}

// ModuleNode executes another flow as a reusable nested module.
type ModuleNode struct {
	BaseNode

	Data ModuleData `json:"data"`
}

// AsModuleNode safely casts an AnyNode to a ModuleNode.
func AsModuleNode(candidate AnyNode) (*ModuleNode, bool) {
	moduleNode, ok := candidate.(*ModuleNode)
	return moduleNode, ok
}

// MustAsModuleNode casts an AnyNode to a ModuleNode, panicking if it fails.
func MustAsModuleNode(candidate AnyNode) *ModuleNode {
	moduleNode, ok := AsModuleNode(candidate)
	if !ok {
		panic("expected ModuleNode but got different type")
	}
	return moduleNode
}

func (n *ModuleNode) GetData() ModuleData {
	return n.Data
}

// InputSchema infers inputs from binding templates.
func (n *ModuleNode) InputSchema() []string {
	vars := (&SchemaInference{}).ExtractTemplateVariables(n.Data.InputBindings)
	sort.Strings(vars)
	return vars
}

// OutputSchema exposes the parent-visible outputs exported by the module node.
func (n *ModuleNode) OutputSchema() []string {
	keys := make([]string, 0, len(n.Data.OutputBindings))
	for key := range n.Data.OutputBindings {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		keys = append(keys, trimmed)
	}
	sort.Strings(keys)
	return keys
}

func (n *ModuleNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()
	flowID := strings.TrimSpace(n.Data.FlowID)
	if flowID == "" {
		// A module node without a flow_id is an incomplete flow definition
		// authored by the user, not a runner fault. The module-graph validation
		// pass intentionally skips empty flow_ids, so this runtime guard is the
		// only place it surfaces. Classify it as a UserError so the node
		// executor logs it at debug rather than error (mirroring
		// engine.ExecuteModule and the module-graph validation path), keeping
		// invalid user flow definitions out of the error stream / error alerts.
		err := spi.NewUserError("MODULE_FLOW_ID_REQUIRED", "module flow_id is required", nil)
		return n.createErrorResult(ctx.Inputs, flowID, err, startTime, nil), err
	}
	if ctx.ModuleResolver == nil {
		err := errors.New("module resolver unavailable")
		return n.createErrorResult(ctx.Inputs, flowID, err, startTime, nil), err
	}
	if ctx.ModuleExecutor == nil {
		err := errors.New("module executor unavailable")
		return n.createErrorResult(ctx.Inputs, flowID, err, startTime, nil), err
	}

	resolvedFlow, ok := ctx.ModuleResolver.ResolveFlow(flowID)
	if !ok {
		resolveErr := fmt.Errorf("referenced flow %q not found", flowID)
		return n.createErrorResult(ctx.Inputs, flowID, resolveErr, startTime, nil), resolveErr
	}

	moduleInputs, err := n.resolveModuleInputs(ctx.Inputs)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, flowID, err, startTime, nil), err
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Str("flowID", flowID).
		Any("moduleInputs", moduleInputs).
		Msg("Starting module node execution")

	childInputs := make(map[string]any, len(ctx.FlowInputs)+len(resolvedFlow.InputOverrides)+len(moduleInputs))
	maps.Copy(childInputs, ctx.FlowInputs)
	maps.Copy(childInputs, resolvedFlow.InputOverrides)
	maps.Copy(childInputs, moduleInputs)

	result, err := ctx.ModuleExecutor.ExecuteModule(ModuleExecutionRequest{
		FlowID:         flowID,
		FlowDefinition: resolvedFlow.FlowDefinition,
		Inputs:         childInputs,
	})
	if err != nil {
		return n.createErrorResult(ctx.Inputs, flowID, err, startTime, result), err
	}

	exportedOutputs, err := n.exportOutputs(result)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, flowID, err, startTime, result), err
	}

	return &ModuleExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeModule,
			Inputs:      ctx.Inputs,
			Outputs:     exportedOutputs,
			ExecutedAt:  time.Now(),
		},
		FlowID:            flowID,
		ChildFinalOutputs: cloneMap(result.FinalOutputs),
		DurationMs:        time.Since(startTime).Milliseconds(),
	}, nil
}

func (n *ModuleNode) resolveModuleInputs(parentInputs map[string]any) (map[string]any, error) {
	resolver := NewTemplateResolver(parentInputs)
	resolved := make(map[string]any, len(n.Data.InputBindings))
	for key, value := range n.Data.InputBindings {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			return nil, errors.New("module input binding key cannot be empty")
		}
		resolvedValue, err := resolver.Resolve(value)
		if err != nil {
			return nil, fmt.Errorf("resolve module input %q: %w", trimmedKey, err)
		}
		resolved[trimmedKey] = resolvedValue
	}
	return resolved, nil
}

func (n *ModuleNode) exportOutputs(result *FlowExecutionResult) (map[string]any, error) {
	outputs := make(map[string]any, len(n.Data.OutputBindings))
	for outputName, sourceRef := range n.Data.OutputBindings {
		trimmedOutputName := strings.TrimSpace(outputName)
		trimmedSourceRef := strings.TrimSpace(sourceRef)
		if trimmedOutputName == "" || trimmedSourceRef == "" {
			return nil, errors.New("module output bindings require non-empty names and references")
		}
		value, ok := result.FinalOutputs[trimmedSourceRef]
		if !ok {
			return nil, fmt.Errorf(
				"module output %q references unavailable child output %q",
				trimmedOutputName,
				trimmedSourceRef,
			)
		}
		outputs[trimmedOutputName] = value
	}
	return outputs, nil
}

func (n *ModuleNode) createErrorResult(
	inputs map[string]any,
	flowID string,
	err error,
	startedAt time.Time,
	childResult *FlowExecutionResult,
) AnyExecutionResult {
	errMsg := err.Error()
	errCode := "MODULE_FAILED"
	childOutputs := map[string]any{}
	if childResult != nil {
		childOutputs = cloneMap(childResult.FinalOutputs)
	}

	return &ModuleExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeModule,
			Inputs:      inputs,
			Outputs:     nil,
			Error:       err,
			ErrorMsg:    &errMsg,
			ErrorCode:   &errCode,
			ExecutedAt:  time.Now(),
		},
		FlowID:            flowID,
		ChildFinalOutputs: childOutputs,
		DurationMs:        time.Since(startedAt).Milliseconds(),
	}
}

func cloneMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(source))
	maps.Copy(cloned, source)
	return cloned
}
