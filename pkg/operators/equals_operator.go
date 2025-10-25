package operators

import "fmt"

// EqualsOperator checks if the actual value equals the expected value
type EqualsOperator struct {
	Expected interface{} `json:"expected"`
}

func (o EqualsOperator) Validate(actual interface{}) (bool, error) {
	// Handle string comparison
	if expectedStr, ok := o.Expected.(string); ok {
		actualStr, ok := actual.(string)
		if !ok {
			return false, fmt.Errorf("expected string but got %T", actual)
		}
		return actualStr == expectedStr, nil
	}

	// Handle numeric comparison
	expectedNum, expectedIsNum := toFloat64(o.Expected)
	actualNum, actualIsNum := toFloat64(actual)

	if expectedIsNum && actualIsNum {
		return actualNum == expectedNum, nil
	}

	// Handle boolean comparison
	if expectedBool, ok := o.Expected.(bool); ok {
		actualBool, ok := actual.(bool)
		if !ok {
			return false, fmt.Errorf("expected boolean but got %T", actual)
		}
		return actualBool == expectedBool, nil
	}

	// Generic comparison
	return actual == o.Expected, nil
}

func (o EqualsOperator) GetType() OperatorType {
	return OperatorTypeEquals
}

// toFloat64 converts various numeric types to float64
func toFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}
