package node

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	_ "github.com/nanostack-dev/echopoint-runner/pkg/extractors/http" // Register HTTP extractors in init()
	"github.com/nanostack-dev/echopoint-runner/pkg/operators"
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

// Evaluate runs the assertion against the response and returns a full result
// record. Index is left zero for the caller to assign. Passed is true only when
// the extractor succeeds and the operator holds; Error is set (and Passed false,
// Actual possibly nil) when the extractor or operator errors. This is the single
// evaluation entry point — it both decides pass/fail and captures what was
// compared, so callers never re-derive the outcome.
func (ca *CompositeAssertion) Evaluate(ctx extractors.ResponseContext) AssertionResult {
	res := AssertionResult{
		Extractor: ca.ExtractorType,
		Operator:  ca.OperatorType,
		Expected:  ca.ExpectedValue,
	}
	if ca.Extractor == nil {
		res.Error = fmt.Sprintf("assertion has no extractor (type %q)", ca.ExtractorType)
		return res
	}
	actual, err := ca.Extractor.Extract(ctx)
	if err != nil {
		res.Error = fmt.Sprintf("extractor %q failed: %v", ca.ExtractorType, err)
		return res
	}
	res.Actual = actual

	passed, err := applyOperator(ca.OperatorType, actual, ca.ExpectedValue)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Passed = passed
	return res
}

// comparator applies an operator to the extracted actual and the expected value.
type comparator func(actual, expected interface{}) (bool, error)

// comparators is the single dispatch table for assertion operators, keyed by the
// shared operators.OperatorType constants so the supported set is checked against
// pkg/operators rather than duplicated string literals. Comparisons are lenient
// (stringify/coerce) because the wire delivers expected values as strings while
// extracted actuals are typed (e.g. an int statusCode vs the string "200").
//
//nolint:gochecknoglobals // immutable operator dispatch table, built once
var comparators = map[operators.OperatorType]comparator{
	operators.OperatorTypeEquals: func(a, e interface{}) (bool, error) {
		return toString(a) == toString(e), nil
	},
	operators.OperatorTypeNotEquals: func(a, e interface{}) (bool, error) {
		return toString(a) != toString(e), nil
	},
	operators.OperatorTypeContains: func(a, e interface{}) (bool, error) {
		return strings.Contains(toString(a), toString(e)), nil
	},
	operators.OperatorTypeNotContains: func(a, e interface{}) (bool, error) {
		return !strings.Contains(toString(a), toString(e)), nil
	},
	operators.OperatorTypeStartsWith: func(a, e interface{}) (bool, error) {
		return strings.HasPrefix(toString(a), toString(e)), nil
	},
	operators.OperatorTypeEndsWith: func(a, e interface{}) (bool, error) {
		return strings.HasSuffix(toString(a), toString(e)), nil
	},
	operators.OperatorTypeRegex: func(a, e interface{}) (bool, error) {
		return regexp.MatchString(toString(e), toString(a))
	},
	operators.OperatorTypeGreaterThan: func(a, e interface{}) (bool, error) {
		return compareNumeric(a, e, func(x, y float64) bool { return x > y })
	},
	operators.OperatorTypeLessThan: func(a, e interface{}) (bool, error) {
		return compareNumeric(a, e, func(x, y float64) bool { return x < y })
	},
	operators.OperatorTypeGreaterThanOrEqual: func(a, e interface{}) (bool, error) {
		return compareNumeric(a, e, func(x, y float64) bool { return x >= y })
	},
	operators.OperatorTypeLessThanOrEqual: func(a, e interface{}) (bool, error) {
		return compareNumeric(a, e, func(x, y float64) bool { return x <= y })
	},
	operators.OperatorTypeBetween: between,
	operators.OperatorTypeEmpty: func(a, _ interface{}) (bool, error) {
		return isEmpty(a), nil
	},
	operators.OperatorTypeNotEmpty: func(a, _ interface{}) (bool, error) {
		return !isEmpty(a), nil
	},
}

func applyOperator(operator string, actual, expected interface{}) (bool, error) {
	cmp, ok := comparators[operators.OperatorType(operator)]
	if !ok {
		return false, fmt.Errorf("unknown assertion operator %q", operator)
	}
	return cmp(actual, expected)
}

// between reports whether actual lies within an inclusive [min, max] range. The
// expected value must be a two-element list (as decoded from operator_data.value).
func between(actual, expected interface{}) (bool, error) {
	bounds, ok := expected.([]interface{})
	if !ok || len(bounds) != 2 {
		return false, fmt.Errorf("between requires a [min, max] pair, got %v", expected)
	}
	value, err := toFloat(actual)
	if err != nil {
		return false, fmt.Errorf("actual value %v is not numeric: %w", actual, err)
	}
	low, err := toFloat(bounds[0])
	if err != nil {
		return false, fmt.Errorf("lower bound %v is not numeric: %w", bounds[0], err)
	}
	high, err := toFloat(bounds[1])
	if err != nil {
		return false, fmt.Errorf("upper bound %v is not numeric: %w", bounds[1], err)
	}
	return value >= low && value <= high, nil
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
