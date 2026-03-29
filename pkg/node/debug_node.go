package node

import (
	"regexp"
	"time"

	"github.com/rs/zerolog/log"
)

type DebugData struct {
	// List of template strings to resolve and log
	Expressions []string `json:"expressions"`
	// Optional label for the debug output
	Label string `json:"label,omitempty"`
}

// DebugNode is a typed node for debugging/logging values during flow execution.
type DebugNode struct {
	BaseNode
	Data DebugData `json:"data"`
}

var debugUnresolvedPattern = regexp.MustCompile(`\{\{[^}]+\}\}`)

// AsDebugNode safely casts an AnyNode to a DebugNode
func AsDebugNode(node AnyNode) (*DebugNode, bool) {
	dNode, ok := node.(*DebugNode)
	return dNode, ok
}

// MustAsDebugNode casts an AnyNode to a DebugNode, panicking if it fails
func MustAsDebugNode(node AnyNode) *DebugNode {
	dNode, ok := AsDebugNode(node)
	if !ok {
		panic("expected DebugNode but got different type")
	}
	return dNode
}

func (n *DebugNode) GetData() DebugData {
	return n.Data
}

func (n *DebugNode) GetOutputs() []Output {
	return n.Outputs
}

func (n *DebugNode) GetAssertions() []CompositeAssertion {
	return n.Assertions
}

// InputSchema returns input keys derived from the expressions
func (n *DebugNode) InputSchema() []string {
	// Simple implementation: scan expressions for {{node.key}} patterns
	// For now, we rely on the generic template resolver which handles this dynamic input.
	// Extract variables per expression so []string is handled correctly.
	si := &SchemaInference{}
	vars := make(map[string]struct{})
	for _, expr := range n.Data.Expressions {
		for _, v := range si.ExtractTemplateVariables(expr) {
			vars[v] = struct{}{}
		}
	}

	result := make([]string, 0, len(vars))
	for v := range vars {
		result = append(result, v)
	}
	return result
}

// OutputSchema for debug nodes is typically empty as they are side-effect only
func (n *DebugNode) OutputSchema() []string {
	return []string{}
}

func (n *DebugNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()

	log.Debug().
		Str("nodeID", n.GetID()).
		Str("label", n.Data.Label).
		Msg("Executing debug node")

	results := make([]DebugResultItem, 0, len(n.Data.Expressions))
	resolver := NewTemplateResolver(ctx.Inputs)

	for _, expr := range n.Data.Expressions {
		resolved, err := resolver.Resolve(expr)

		item := DebugResultItem{
			Expression: expr,
		}

		if err != nil {
			log.Warn().
				Str("nodeID", n.GetID()).
				Str("expression", expr).
				Err(err).
				Msg("Failed to resolve debug expression")
			item.Error = err.Error()
			results = append(results, item)
			continue
		}

		if resolvedStr, ok := resolved.(string); ok && debugUnresolvedPattern.MatchString(resolvedStr) {
			item.Error = "unresolved template variables"
		} else {
			item.Value = resolved
		}
		results = append(results, item)
	}

	// Create typed DebugExecutionResult
	result := &DebugExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeDebug,
			Inputs:      ctx.Inputs,
			Outputs:     nil, // Debug nodes don't typically produce downstream outputs
			ExecutedAt:  time.Now(),
		},
		Results:    results,
		DurationMs: time.Since(startTime).Milliseconds(),
	}

	log.Info().
		Str("nodeID", n.GetID()).
		Int("count", len(results)).
		Msg("Debug node execution complete")

	return result, nil
}
