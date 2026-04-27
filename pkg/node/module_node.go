package node

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type ModuleData struct {
	FlowID         string                 `json:"flow_id"`
	InputBindings  map[string]interface{} `json:"input_bindings,omitempty"`
	OutputBindings map[string]string      `json:"output_bindings,omitempty"`
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
	if strings.TrimSpace(n.Data.FlowID) == "" {
		err := fmt.Errorf("module flow_id is required")
		return n.createErrorResult(ctx.Inputs, err, startTime, nil), err
	}
	if ctx.ModuleResolver == nil {
		err := fmt.Errorf("module resolver unavailable")
		return n.createErrorResult(ctx.Inputs, err, startTime, nil), err
	}
	if ctx.ModuleExecutor == nil {
		err := fmt.Errorf("module executor unavailable")
		return n.createErrorResult(ctx.Inputs, err, startTime, nil), err
	}

	resolvedFlow, ok := ctx.ModuleResolver.ResolveFlow(n.Data.FlowID)
	if !ok {
		err := fmt.Errorf("referenced flow %q not found", n.Data.FlowID)
		return n.createErrorResult(ctx.Inputs, err, startTime, nil), err
	}

	moduleInputs, err := n.resolveModuleInputs(ctx.Inputs)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, err, startTime, nil), err
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Str("flowID", n.Data.FlowID).
		Any("moduleInputs", moduleInputs).
		Msg("Starting module node execution")

	childInputs := make(map[string]interface{}, len(resolvedFlow.Environment)+len(moduleInputs))
	for key, value := range resolvedFlow.Environment {
		childInputs[key] = value
	}
	for key, value := range moduleInputs {
		childInputs[key] = value
	}

	result, err := ctx.ModuleExecutor.ExecuteModule(ModuleExecutionRequest{
		FlowID:         n.Data.FlowID,
		FlowDefinition: resolvedFlow.FlowDefinition,
		Inputs:         childInputs,
	})
	if err != nil {
		return n.createErrorResult(ctx.Inputs, err, startTime, result), err
	}

	exportedOutputs, err := n.exportOutputs(result)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, err, startTime, result), err
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
		FlowID:            n.Data.FlowID,
		ChildFinalOutputs: cloneMap(result.FinalOutputs),
		DurationMs:        time.Since(startTime).Milliseconds(),
	}, nil
}

func (n *ModuleNode) resolveModuleInputs(parentInputs map[string]interface{}) (map[string]interface{}, error) {
	resolver := NewTemplateResolver(parentInputs)
	resolved := make(map[string]interface{}, len(n.Data.InputBindings))
	for key, value := range n.Data.InputBindings {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			return nil, fmt.Errorf("module input binding key cannot be empty")
		}
		resolvedValue, err := resolver.Resolve(value)
		if err != nil {
			return nil, fmt.Errorf("resolve module input %q: %w", trimmedKey, err)
		}
		resolved[trimmedKey] = resolvedValue
	}
	return resolved, nil
}

func (n *ModuleNode) exportOutputs(result *FlowExecutionResult) (map[string]interface{}, error) {
	outputs := make(map[string]interface{}, len(n.Data.OutputBindings))
	for outputName, sourceRef := range n.Data.OutputBindings {
		trimmedOutputName := strings.TrimSpace(outputName)
		trimmedSourceRef := strings.TrimSpace(sourceRef)
		if trimmedOutputName == "" || trimmedSourceRef == "" {
			return nil, fmt.Errorf("module output bindings require non-empty names and references")
		}
		value, ok := result.FinalOutputs[trimmedSourceRef]
		if !ok {
			return nil, fmt.Errorf("module output %q references unavailable child output %q", trimmedOutputName, trimmedSourceRef)
		}
		outputs[trimmedOutputName] = value
	}
	return outputs, nil
}

func (n *ModuleNode) createErrorResult(
	inputs map[string]interface{},
	err error,
	startedAt time.Time,
	childResult *FlowExecutionResult,
) AnyExecutionResult {
	errMsg := err.Error()
	errCode := "MODULE_FAILED"
	childOutputs := map[string]interface{}{}
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
		FlowID:            n.Data.FlowID,
		ChildFinalOutputs: childOutputs,
		DurationMs:        time.Since(startedAt).Milliseconds(),
	}
}

func cloneMap(source map[string]interface{}) map[string]interface{} {
	if len(source) == 0 {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
