package operators

import (
	"fmt"
	"regexp"
	"strings"
)

// StartsWithOperator checks if the actual string starts with the expected prefix.
type StartsWithOperator struct {
	Prefix string `json:"prefix"`
}

func (o StartsWithOperator) Validate(actual interface{}) (bool, error) {
	actualStr, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("startsWith operator requires string, got %T", actual)
	}

	return strings.HasPrefix(actualStr, o.Prefix), nil
}

func (o StartsWithOperator) GetType() OperatorType {
	return OperatorTypeStartsWith
}

// EndsWithOperator checks if the actual string ends with the expected suffix.
type EndsWithOperator struct {
	Suffix string `json:"suffix"`
}

func (o EndsWithOperator) Validate(actual interface{}) (bool, error) {
	actualStr, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("endsWith operator requires string, got %T", actual)
	}

	return strings.HasSuffix(actualStr, o.Suffix), nil
}

func (o EndsWithOperator) GetType() OperatorType {
	return OperatorTypeEndsWith
}

// RegexOperator checks if the actual string matches the expected regex pattern.
type RegexOperator struct {
	Pattern string `json:"pattern"`
}

func (o RegexOperator) Validate(actual interface{}) (bool, error) {
	actualStr, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("regex operator requires string, got %T", actual)
	}

	matched, err := regexp.MatchString(o.Pattern, actualStr)
	if err != nil {
		return false, fmt.Errorf("invalid regex pattern: %w", err)
	}

	return matched, nil
}

func (o RegexOperator) GetType() OperatorType {
	return OperatorTypeRegex
}

// EmptyOperator checks if the actual string is empty.
type EmptyOperator struct{}

func (o EmptyOperator) Validate(actual interface{}) (bool, error) {
	actualStr, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("empty operator requires string, got %T", actual)
	}

	return actualStr == "", nil
}

func (o EmptyOperator) GetType() OperatorType {
	return OperatorTypeEmpty
}

// NotEmptyOperator checks if the actual string is not empty.
type NotEmptyOperator struct{}

func (o NotEmptyOperator) Validate(actual interface{}) (bool, error) {
	actualStr, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("notEmpty operator requires string, got %T", actual)
	}

	return actualStr != "", nil
}

func (o NotEmptyOperator) GetType() OperatorType {
	return OperatorTypeNotEmpty
}

// NotEqualsOperator checks if the actual value does not equal the expected value.
type NotEqualsOperator struct {
	Expected interface{} `json:"expected"`
}

func (o NotEqualsOperator) Validate(actual interface{}) (bool, error) {
	equals := EqualsOperator(o)
	result, err := equals.Validate(actual)
	if err != nil {
		return false, err
	}
	return !result, nil
}

func (o NotEqualsOperator) GetType() OperatorType {
	return OperatorTypeNotEquals
}

// NotContainsOperator checks if the actual value does not contain the expected substring.
type NotContainsOperator struct {
	Substring string `json:"substring"`
}

func (o NotContainsOperator) Validate(actual interface{}) (bool, error) {
	contains := ContainsOperator(o)
	result, err := contains.Validate(actual)
	if err != nil {
		return false, err
	}
	return !result, nil
}

func (o NotContainsOperator) GetType() OperatorType {
	return OperatorTypeNotContains
}
