package node

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	_ "github.com/nanostack-dev/echopoint-runner/pkg/extractors/http" // Register HTTP extractors in init()
)

// CompositeAssertion combines an extractor with an operator for validation.
type CompositeAssertion struct {
	Extractor     extractors.AnyExtractor `json:"-"`             // The actual extractor instance
	Operator      interface{}             `json:"-"`             // The actual operator instance (for future use)
	ExtractorType string                  `json:"extractorType"` // jsonPath, xmlPath, statusCode, header, body
	ExtractorData interface{}             `json:"extractorData"` // Configuration for the extractor
	OperatorType  string                  `json:"operatorType"`  // equals, contains, greaterThan, etc.
	OperatorData  interface{}             `json:"operatorData"`  // Configuration for the operator
	ExpectedValue interface{}             `json:"-"`             // Resolved expected value (operator_data.value)
}

// assertionWire is the on-the-wire shape produced by the echopoint backend.
// Assertions are stored snake_case (extractor_type/extractor_data,
// operator_type/operator_data); older callers may send a nested extractor object.
type assertionWire struct {
	Extractor json.RawMessage `json:"extractor"`
	Operator  json.RawMessage `json:"operator"`

	ExtractorType string          `json:"extractor_type"`
	ExtractorData json.RawMessage `json:"extractor_data"`
	OperatorType  string          `json:"operator_type"`
	OperatorData  json.RawMessage `json:"operator_data"`

	// camelCase fallbacks
	ExtractorTypeCamel string          `json:"extractorType"`
	ExtractorDataCamel json.RawMessage `json:"extractorData"`
	OperatorTypeCamel  string          `json:"operatorType"`
	OperatorDataCamel  json.RawMessage `json:"operatorData"`
}

// UnmarshalJSON implements custom unmarshaling for CompositeAssertion.
func (ca *CompositeAssertion) UnmarshalJSON(data []byte) error {
	var w assertionWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}

	ca.ExtractorType = firstNonEmpty(w.ExtractorType, w.ExtractorTypeCamel)
	ca.OperatorType = firstNonEmpty(w.OperatorType, w.OperatorTypeCamel)
	extractorData := firstRaw(w.ExtractorData, w.ExtractorDataCamel)

	// Build the extractor. Prefer a nested "extractor" object; otherwise
	// synthesize one from extractor_type + extractor_data (a flat {type, ...data}).
	extractorJSON := w.Extractor
	if len(extractorJSON) == 0 && ca.ExtractorType != "" {
		synth, err := synthesizeExtractorJSON(ca.ExtractorType, extractorData)
		if err != nil {
			return err
		}
		extractorJSON = synth
	}
	if len(extractorJSON) > 0 {
		extractor, err := extractors.UnmarshalExtractor(extractorJSON)
		if err != nil {
			return fmt.Errorf("failed to unmarshal assertion extractor: %w", err)
		}
		ca.Extractor = extractor
	}

	// Resolve the expected value from operator_data.value.
	operatorData := firstRaw(w.OperatorData, w.OperatorDataCamel)
	if len(operatorData) > 0 {
		var od struct {
			Value interface{} `json:"value"`
		}
		if err := json.Unmarshal(operatorData, &od); err != nil {
			return fmt.Errorf("failed to unmarshal assertion operator data: %w", err)
		}
		ca.ExpectedValue = od.Value
		ca.OperatorData = od
	}

	return nil
}

// synthesizeExtractorJSON merges the legacy extractor_data object with the
// extractor type into the flat {"type": ..., ...data} shape UnmarshalExtractor expects.
func synthesizeExtractorJSON(extractorType string, extractorData json.RawMessage) (json.RawMessage, error) {
	merged := map[string]interface{}{}
	if len(extractorData) > 0 {
		if err := json.Unmarshal(extractorData, &merged); err != nil {
			return nil, fmt.Errorf("failed to unmarshal extractor data: %w", err)
		}
	}
	merged["type"] = extractorType
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Evaluate extracts the actual value from the response and applies the operator
// against the expected value. Returns true when the assertion holds.
func (ca *CompositeAssertion) Evaluate(ctx extractors.ResponseContext) (bool, error) {
	if ca.Extractor == nil {
		return false, fmt.Errorf("assertion has no extractor (type %q)", ca.ExtractorType)
	}
	actual, err := ca.Extractor.Extract(ctx)
	if err != nil {
		return false, fmt.Errorf("extractor %q failed: %w", ca.ExtractorType, err)
	}
	return applyOperator(ca.OperatorType, actual, ca.ExpectedValue)
}

func applyOperator(operator string, actual, expected interface{}) (bool, error) {
	switch operator {
	case "equals":
		return toString(actual) == toString(expected), nil
	case "notEquals":
		return toString(actual) != toString(expected), nil
	case "contains":
		return strings.Contains(toString(actual), toString(expected)), nil
	case "notContains":
		return !strings.Contains(toString(actual), toString(expected)), nil
	case "greaterThan":
		return compareNumeric(actual, expected, func(a, b float64) bool { return a > b })
	case "lessThan":
		return compareNumeric(actual, expected, func(a, b float64) bool { return a < b })
	case "empty":
		return isEmpty(actual), nil
	case "notEmpty":
		return !isEmpty(actual), nil
	default:
		return false, fmt.Errorf("unknown assertion operator %q", operator)
	}
}

func compareNumeric(actual, expected interface{}, cmp func(a, b float64) bool) (bool, error) {
	a, err := toFloat(actual)
	if err != nil {
		return false, fmt.Errorf("actual value %v is not numeric: %w", actual, err)
	}
	b, err := toFloat(expected)
	if err != nil {
		return false, fmt.Errorf("expected value %v is not numeric: %w", expected, err)
	}
	return cmp(a, b), nil
}

func isEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case string:
		return t == ""
	case []interface{}:
		return len(t) == 0
	case map[string]interface{}:
		return len(t) == 0
	default:
		return toString(v) == ""
	}
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toFloat(v interface{}) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case string:
		return strconv.ParseFloat(t, 64)
	default:
		return strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstRaw(a, b json.RawMessage) json.RawMessage {
	if len(a) > 0 {
		return a
	}
	return b
}
