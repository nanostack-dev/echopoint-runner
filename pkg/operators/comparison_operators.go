package operators

import "fmt"

// GreaterThanOperator checks if the actual value is greater than the expected value
type GreaterThanOperator struct {
	Expected float64 `json:"expected"`
}

func (o GreaterThanOperator) Validate(actual interface{}) (bool, error) {
	actualNum, ok := toFloat64(actual)
	if !ok {
		return false, fmt.Errorf("greaterThan operator requires numeric value, got %T", actual)
	}

	return actualNum > o.Expected, nil
}

func (o GreaterThanOperator) GetType() OperatorType {
	return OperatorTypeGreaterThan
}

// LessThanOperator checks if the actual value is less than the expected value
type LessThanOperator struct {
	Expected float64 `json:"expected"`
}

func (o LessThanOperator) Validate(actual interface{}) (bool, error) {
	actualNum, ok := toFloat64(actual)
	if !ok {
		return false, fmt.Errorf("lessThan operator requires numeric value, got %T", actual)
	}

	return actualNum < o.Expected, nil
}

func (o LessThanOperator) GetType() OperatorType {
	return OperatorTypeLessThan
}

// GreaterThanOrEqualOperator checks if the actual value is greater than or equal to the expected value
type GreaterThanOrEqualOperator struct {
	Expected float64 `json:"expected"`
}

func (o GreaterThanOrEqualOperator) Validate(actual interface{}) (bool, error) {
	actualNum, ok := toFloat64(actual)
	if !ok {
		return false, fmt.Errorf("greaterThanOrEqual operator requires numeric value, got %T", actual)
	}

	return actualNum >= o.Expected, nil
}

func (o GreaterThanOrEqualOperator) GetType() OperatorType {
	return OperatorTypeGreaterThanOrEqual
}

// LessThanOrEqualOperator checks if the actual value is less than or equal to the expected value
type LessThanOrEqualOperator struct {
	Expected float64 `json:"expected"`
}

func (o LessThanOrEqualOperator) Validate(actual interface{}) (bool, error) {
	actualNum, ok := toFloat64(actual)
	if !ok {
		return false, fmt.Errorf("lessThanOrEqual operator requires numeric value, got %T", actual)
	}

	return actualNum <= o.Expected, nil
}

func (o LessThanOrEqualOperator) GetType() OperatorType {
	return OperatorTypeLessThanOrEqual
}

// BetweenOperator checks if the actual value is between min and max (inclusive)
type BetweenOperator struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

func (o BetweenOperator) Validate(actual interface{}) (bool, error) {
	actualNum, ok := toFloat64(actual)
	if !ok {
		return false, fmt.Errorf("between operator requires numeric value, got %T", actual)
	}

	return actualNum >= o.Min && actualNum <= o.Max, nil
}

func (o BetweenOperator) GetType() OperatorType {
	return OperatorTypeBetween
}
