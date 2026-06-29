package node

import (
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
)

// SetVariableData configures a set-variable node. Each entry maps an output name
// to a template value: a string with {{a.b}} / {{x}} references, a number/bool,
// or a nested object/array that may contain {{{raw}}} structured references.
type SetVariableData struct {
	Variables map[string]any `json:"variables"`
}

// SetVariableNode computes named outputs from upstream node outputs and flow
// inputs via template resolution, without performing any HTTP call. It is used
// to assemble payloads, derive headers, and reshape values between nodes.
type SetVariableNode struct {
	BaseNode

	Data SetVariableData `json:"data"`
}

// AsSetVariableNode safely casts an AnyNode to a SetVariableNode.
// Returns the SetVariableNode and true if the cast succeeds, nil and false otherwise.
func AsSetVariableNode(candidate AnyNode) (*SetVariableNode, bool) {
	setVariableNode, ok := candidate.(*SetVariableNode)
	return setVariableNode, ok
}

// MustAsSetVariableNode casts an AnyNode to a SetVariableNode, panicking if it fails.
// Use this when you're certain the node is a SetVariableNode.
func MustAsSetVariableNode(candidate AnyNode) *SetVariableNode {
	setVariableNode, ok := AsSetVariableNode(candidate)
	if !ok {
		panic("expected SetVariableNode but got different type")
	}
	return setVariableNode
}

func (n *SetVariableNode) GetData() SetVariableData {
	return n.Data
}

// InputSchema infers required inputs from the template variables referenced in
// the configured values, mirroring how ModuleNode derives inputs from bindings.
func (n *SetVariableNode) InputSchema() []string {
	vars := (&SchemaInference{}).ExtractTemplateVariables(n.Data.Variables)
	sort.Strings(vars)
	return vars
}

// OutputSchema exposes the names of the variables this node produces, sorted.
func (n *SetVariableNode) OutputSchema() []string {
	keys := make([]string, 0, len(n.Data.Variables))
	for key := range n.Data.Variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Execute resolves every configured variable template against the node inputs
// and dynamic variables, returning the resolved values as the node outputs. No
// HTTP call or external side effect is performed.
func (n *SetVariableNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("variableCount", len(n.Data.Variables)).
		Msg("Starting set-variable node execution")

	resolver := NewTemplateResolverWithDynamics(ctx.Inputs, ctx.DynamicVars)
	outputs := make(map[string]any, len(n.Data.Variables))
	for name, val := range n.Data.Variables {
		resolved, err := resolver.Resolve(val)
		if err != nil {
			wrapped := fmt.Errorf("resolve set-variable %q: %w", name, err)
			log.Error().
				Str("nodeID", n.GetID()).
				Str("variable", name).
				Err(wrapped).
				Msg("Set-variable node failed to resolve value")
			return n.createErrorResult(ctx.Inputs, wrapped, startTime), wrapped
		}
		outputs[name] = resolved
	}

	result := &SetVariableExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeSetVariable,
			Inputs:      ctx.Inputs,
			Outputs:     outputs,
			ExecutedAt:  time.Now(),
		},
		DurationMs: time.Since(startTime).Milliseconds(),
	}

	log.Info().
		Str("nodeID", n.GetID()).
		Int("variableCount", len(outputs)).
		Int64("durationMs", result.DurationMs).
		Msg("Set-variable node executed successfully")

	return result, nil
}

func (n *SetVariableNode) createErrorResult(
	inputs map[string]any,
	err error,
	startedAt time.Time,
) AnyExecutionResult {
	errMsg := err.Error()
	errCode := "SET_VARIABLE_FAILED"

	return &SetVariableExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeSetVariable,
			Inputs:      inputs,
			Outputs:     nil,
			Error:       err,
			ErrorMsg:    &errMsg,
			ErrorCode:   &errCode,
			ExecutedAt:  time.Now(),
		},
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
}
