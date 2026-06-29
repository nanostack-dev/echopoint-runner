package operators

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Comparator applies an operator to an extracted actual value and the assertion's
// expected value. Comparisons are lenient (stringify/coerce) because the wire
// delivers expected values as strings while extracted actuals are typed
// (e.g. an int statusCode vs the string "200").
type Comparator func(actual, expected any) (bool, error)

// comparators is the operator dispatch registry. Built-ins are registered below;
// additional operators register via Register without editing this core.
//
//nolint:gochecknoglobals // immutable-after-init operator dispatch registry
var comparators = map[OperatorType]Comparator{
	OperatorTypeEquals: func(a, e any) (bool, error) {
		return toString(a) == toString(e), nil
	},
	OperatorTypeNotEquals: func(a, e any) (bool, error) {
		return toString(a) != toString(e), nil
	},
	OperatorTypeContains: func(a, e any) (bool, error) {
		return strings.Contains(toString(a), toString(e)), nil
	},
	OperatorTypeNotContains: func(a, e any) (bool, error) {
		return !strings.Contains(toString(a), toString(e)), nil
	},
	OperatorTypeStartsWith: func(a, e any) (bool, error) {
		return strings.HasPrefix(toString(a), toString(e)), nil
	},
	OperatorTypeEndsWith: func(a, e any) (bool, error) {
		return strings.HasSuffix(toString(a), toString(e)), nil
	},
	OperatorTypeRegex: func(a, e any) (bool, error) {
		return regexp.MatchString(toString(e), toString(a))
	},
	OperatorTypeGreaterThan: func(a, e any) (bool, error) {
		return compareNumeric(a, e, func(x, y float64) bool { return x > y })
	},
	OperatorTypeLessThan: func(a, e any) (bool, error) {
		return compareNumeric(a, e, func(x, y float64) bool { return x < y })
	},
	OperatorTypeGreaterThanOrEqual: func(a, e any) (bool, error) {
		return compareNumeric(a, e, func(x, y float64) bool { return x >= y })
	},
	OperatorTypeLessThanOrEqual: func(a, e any) (bool, error) {
		return compareNumeric(a, e, func(x, y float64) bool { return x <= y })
	},
	OperatorTypeBetween: between,
	OperatorTypeEmpty: func(a, _ any) (bool, error) {
		return isEmpty(a), nil
	},
	OperatorTypeNotEmpty: func(a, _ any) (bool, error) {
		return !isEmpty(a), nil
	},
}

// Register adds (or overrides) the comparator for an operator type. Call from an
// init() so a new operator becomes available without editing the dispatch core.
func Register(operatorType OperatorType, comparator Comparator) {
	comparators[operatorType] = comparator
}

// Compare applies the registered comparator for operatorType, returning an error
// when the operator is unknown.
//
// Coercion contract: comparisons are lenient. equals/notEquals/contains and the
// other string operators compare the stringified forms (toString), so the
// integer 200 and the string "200" are equal; the numeric operators
// (greaterThan/between/...) coerce both sides via toFloat. This matches the wire,
// which delivers expected values as strings. It is a deliberate, tested contract,
// not type-aware equality.
func Compare(operatorType OperatorType, actual, expected any) (bool, error) {
	comparator, ok := comparators[operatorType]
	if !ok {
		return false, fmt.Errorf("unknown assertion operator %q", operatorType)
	}
	return comparator(actual, expected)
}

// IsKnown reports whether operatorType has a registered comparator. Callers use
// it to reject an unknown operator at flow-decode time (mirroring the extractor
// registry) instead of failing deep inside Compare during execution.
func IsKnown(operatorType OperatorType) bool {
	_, ok := comparators[operatorType]
	return ok
}

// between reports whether actual lies within an inclusive [min, max] range. The
// expected value must be a two-element list (as decoded from operator_data.value).
func between(actual, expected any) (bool, error) {
	bounds, ok := expected.([]any)
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

func compareNumeric(actual, expected any, cmp func(a, b float64) bool) (bool, error) {
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

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	default:
		return toString(v) == ""
	}
}

func toString(v any) string {
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

func toFloat(v any) (float64, error) {
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
