package node

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// OutputProperty describes one JSON Schema property on an output node.
// Value is required and holds the literal or template expression producing
// the field.
type OutputProperty struct {
	Type        string `json:"type,omitempty"`
	Format      string `json:"format,omitempty"`
	Description string `json:"description,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
	Value       any    `json:"value"`
	Secret      bool   `json:"secret,omitempty"`
}

// OutputData configures an output node using JSON Schema properties whose
// Value templates assemble the function's returned fields.
type OutputData struct {
	Properties map[string]OutputProperty `json:"properties"`
}

// OutputNode resolves and validates the named output contract of a function
// flow once upstream steps have succeeded.
type OutputNode struct {
	BaseNode

	Data OutputData `json:"data"`
}

// AsOutputNode safely casts an AnyNode to an OutputNode.
func AsOutputNode(candidate AnyNode) (*OutputNode, bool) {
	outputNode, ok := candidate.(*OutputNode)
	return outputNode, ok
}

func (n *OutputNode) GetData() OutputData {
	return n.Data
}

// Validate checks static output property invariants at parse time.
func (n *OutputNode) Validate() error {
	keys := make([]string, 0, len(n.Data.Properties))
	for key := range n.Data.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return fmt.Errorf("output node %s: property name cannot be empty", n.GetID())
		}
		if n.Data.Properties[key].Value == nil {
			return fmt.Errorf("output node %s: property %q requires a value", n.GetID(), trimmed)
		}
	}
	return nil
}

// InputSchema infers all template references used inside the output properties'
// Value fields.
func (n *OutputNode) InputSchema() []string {
	values := make(map[string]any, len(n.Data.Properties))
	for key, prop := range n.Data.Properties {
		values[key] = prop.Value
	}
	vars := (&SchemaInference{}).ExtractTemplateVariables(values)
	sort.Strings(vars)
	return vars
}

// OutputSchema returns the sorted names of the declared output properties.
func (n *OutputNode) OutputSchema() []string {
	keys := make([]string, 0, len(n.Data.Properties))
	for key := range n.Data.Properties {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	return keys
}

func (n *OutputNode) Execute(ctx spi.ExecutionContext) (spi.AnyResult, error) {
	startTime := time.Now()
	resolver := NewTemplateResolverWithDynamics(ctx.Inputs, ctx.DynamicVars)
	keys := n.OutputSchema()
	outputs := make(map[string]any, len(keys))
	si := &SchemaInference{}

	for _, name := range keys {
		prop := n.Data.Properties[name]
		if prop.Value == nil {
			userErr := spi.NewUserError(
				"OUTPUT_VALUE_MISSING",
				fmt.Sprintf("output %q requires a value", name),
				nil,
			)
			return n.createErrorResult(ctx.Inputs, userErr, startTime), userErr
		}
		resolved, err := resolver.Resolve(prop.Value)
		if err != nil {
			userErr := spi.NewUserError(
				"OUTPUT_RESOLUTION_FAILED",
				fmt.Sprintf("resolve output %q: %v", name, err),
				err,
			)
			return n.createErrorResult(ctx.Inputs, userErr, startTime), userErr
		}
		constraint := schemaConstraint{Type: prop.Type, Format: prop.Format, Enum: prop.Enum}
		if validateErr := validateSchemaProperty(name, resolved, constraint); validateErr != nil {
			userErr := spi.NewUserError("OUTPUT_INVALID", validateErr.Error(), validateErr)
			return n.createErrorResult(ctx.Inputs, userErr, startTime), userErr
		}
		if prop.Secret {
			redact.LearnOutput(ctx.Context(), n.GetID(), name, resolved)
		}
		for _, ref := range si.ExtractTemplateVariables(prop.Value) {
			if redact.Withheld(ctx.Context(), strings.TrimSpace(ref)) {
				redact.Withhold(ctx.Context(), n.GetID(), name)
			}
		}
		outputs[name] = resolved
	}

	return &OutputExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    spi.KindOutput,
			Inputs:      ctx.Inputs,
			Outputs:     outputs,
			ExecutedAt:  time.Now(),
		},
		DurationMs: time.Since(startTime).Milliseconds(),
	}, nil
}

func (n *OutputNode) createErrorResult(
	inputs map[string]any,
	err error,
	startedAt time.Time,
) spi.AnyResult {
	code := "OUTPUT_INVALID"
	if userErr, ok := spi.AsUserError(err); ok {
		code = userErr.Code
	}
	return &OutputExecutionResult{
		BaseExecutionResult: failedNodeBase(n.BaseNode, inputs, err, failureDetails{
			Kind: spi.KindOutput, Code: code, Message: err.Error(),
		}),
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
}
