package node

import (
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

var uuidFormatPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// InputProperty describes one JSON Schema property on an input node.
type InputProperty struct {
	Type        string `json:"type,omitempty"`
	Format      string `json:"format,omitempty"`
	Description string `json:"description,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
	Default     any    `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
}

// schemaConstraint holds the validation fields shared by input and output
// JSON Schema properties.
type schemaConstraint struct {
	Type   string
	Format string
	Enum   []any
}

// InputData configures an input node using a JSON Schema object shape.
type InputData struct {
	Properties map[string]InputProperty `json:"properties"`
	Required   []string                 `json:"required,omitempty"`
}

// InputNode validates and exposes caller-supplied function inputs as node
// outputs ({{input.<name>}}), applying defaults and secret redaction.
type InputNode struct {
	BaseNode

	Data InputData `json:"data"`
}

// AsInputNode safely casts an AnyNode to an InputNode.
func AsInputNode(candidate AnyNode) (*InputNode, bool) {
	inputNode, ok := candidate.(*InputNode)
	return inputNode, ok
}

func (n *InputNode) GetData() InputData {
	return n.Data
}

// InputSchema returns nil because an input node reads caller values from
// ctx.FlowInputs (or property defaults) at runtime rather than requiring static
// initial_inputs at parse time.
func (n *InputNode) InputSchema() []string {
	return nil
}

// OutputSchema returns the sorted names of the declared input properties.
func (n *InputNode) OutputSchema() []string {
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

func (n *InputNode) isRequired(name string, prop InputProperty) bool {
	return prop.Required || slices.Contains(n.Data.Required, name)
}

func (n *InputNode) Execute(ctx spi.ExecutionContext) (spi.AnyResult, error) {
	startTime := time.Now()
	keys := n.OutputSchema()
	outputs := make(map[string]any, len(keys))

	for _, name := range keys {
		prop := n.Data.Properties[name]
		val, present := ctx.FlowInputs[name]
		if !present || val == nil {
			if prop.Default != nil {
				val = prop.Default
				present = true
			}
		}
		if (!present || val == nil) && n.isRequired(name, prop) {
			userErr := spi.NewUserError(
				"INPUT_REQUIRED",
				fmt.Sprintf("input %q is required", name),
				nil,
			)
			return n.createErrorResult(ctx.Inputs, userErr, startTime), userErr
		}
		if present && val != nil {
			constraint := schemaConstraint{Type: prop.Type, Format: prop.Format, Enum: prop.Enum}
			if err := validateSchemaProperty(name, val, constraint); err != nil {
				userErr := spi.NewUserError("INPUT_INVALID", err.Error(), err)
				return n.createErrorResult(ctx.Inputs, userErr, startTime), userErr
			}
			if prop.Secret {
				redact.LearnOutput(ctx.Context(), n.GetID(), name, val)
			}
			outputs[name] = val
		}
	}

	return &InputExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    spi.KindInput,
			Inputs:      ctx.Inputs,
			Outputs:     outputs,
			ExecutedAt:  time.Now(),
		},
		DurationMs: time.Since(startTime).Milliseconds(),
	}, nil
}

func (n *InputNode) createErrorResult(
	inputs map[string]any,
	err error,
	startedAt time.Time,
) spi.AnyResult {
	code := "INPUT_INVALID"
	if userErr, ok := spi.AsUserError(err); ok {
		code = userErr.Code
	}
	return &InputExecutionResult{
		BaseExecutionResult: failedNodeBase(n.BaseNode, inputs, err, failureDetails{
			Kind: spi.KindInput, Code: code, Message: err.Error(),
		}),
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
}

func validateSchemaProperty(name string, val any, prop schemaConstraint) error {
	if err := validatePropertyType(name, val, prop.Type); err != nil {
		return err
	}
	if prop.Format != "" {
		if err := validatePropertyFormat(name, val, prop.Format); err != nil {
			return err
		}
	}
	if len(prop.Enum) > 0 && !matchesEnum(val, prop.Enum) {
		return fmt.Errorf("property %q value is not one of the allowed enum values", name)
	}
	return nil
}

func validatePropertyType(name string, val any, schemaType string) error {
	switch strings.ToLower(strings.TrimSpace(schemaType)) {
	case "", "any":
		return nil
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("property %q must be a string", name)
		}
	case "integer":
		if !isIntegerValue(val) {
			return fmt.Errorf("property %q must be an integer", name)
		}
	case "number":
		if !isNumberValue(val) {
			return fmt.Errorf("property %q must be a number", name)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("property %q must be a boolean", name)
		}
	case "array":
		rv := reflect.ValueOf(val)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return fmt.Errorf("property %q must be an array", name)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("property %q must be an object", name)
		}
	default:
		return fmt.Errorf("property %q has unsupported schema type %q", name, schemaType)
	}
	return nil
}

func isIntegerValue(val any) bool {
	switch v := val.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Trunc(v) == v
	case float32:
		f := float64(v)
		return !math.IsNaN(f) && !math.IsInf(f, 0) && math.Trunc(f) == f
	case json.Number:
		_, err := v.Int64()
		return err == nil
	default:
		return false
	}
}

func isNumberValue(val any) bool {
	if isIntegerValue(val) {
		return true
	}
	switch v := val.(type) {
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case float32:
		f := float64(v)
		return !math.IsNaN(f) && !math.IsInf(f, 0)
	case json.Number:
		_, err := v.Float64()
		return err == nil
	default:
		return false
	}
}

func validatePropertyFormat(name string, val any, format string) error {
	str, ok := val.(string)
	if !ok {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "email":
		addr, err := mail.ParseAddress(str)
		if err != nil || addr.Address != str {
			return fmt.Errorf("property %q must be a valid email address", name)
		}
	case "uuid":
		if !uuidFormatPattern.MatchString(str) {
			return fmt.Errorf("property %q must be a valid UUID", name)
		}
	case "uri", "url":
		parsed, err := url.ParseRequestURI(str)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("property %q must be a valid URI", name)
		}
	case "date":
		if _, err := time.Parse(time.DateOnly, str); err != nil {
			return fmt.Errorf("property %q must be a valid YYYY-MM-DD date", name)
		}
	case "date-time":
		if _, err := time.Parse(time.RFC3339, str); err != nil {
			return fmt.Errorf("property %q must be a valid RFC3339 date-time", name)
		}
	}
	return nil
}

func matchesEnum(val any, allowed []any) bool {
	valJSON, err := json.Marshal(val)
	if err != nil {
		return false
	}
	for _, candidate := range allowed {
		candJSON, candErr := json.Marshal(candidate)
		if candErr == nil && string(valJSON) == string(candJSON) {
			return true
		}
	}
	return false
}
