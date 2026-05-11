package operators

// Operator defines the interface for all validation operators.
type Operator interface {
	// Validate checks if the actual value passes the operator's validation logic
	Validate(actual interface{}) (bool, error)

	// GetType returns the operator type identifier
	GetType() OperatorType
}

type OperatorType string

const (
	OperatorTypeEquals             OperatorType = "equals"
	OperatorTypeNotEquals          OperatorType = "notEquals"
	OperatorTypeContains           OperatorType = "contains"
	OperatorTypeNotContains        OperatorType = "notContains"
	OperatorTypeStartsWith         OperatorType = "startsWith"
	OperatorTypeEndsWith           OperatorType = "endsWith"
	OperatorTypeRegex              OperatorType = "regex"
	OperatorTypeEmpty              OperatorType = "empty"
	OperatorTypeNotEmpty           OperatorType = "notEmpty"
	OperatorTypeGreaterThan        OperatorType = "greaterThan"
	OperatorTypeLessThan           OperatorType = "lessThan"
	OperatorTypeGreaterThanOrEqual OperatorType = "greaterThanOrEqual"
	OperatorTypeLessThanOrEqual    OperatorType = "lessThanOrEqual"
	OperatorTypeBetween            OperatorType = "between"
)

// ValueType represents the type of value an operator can work with.
type ValueType string

const (
	ValueTypeString  ValueType = "string"
	ValueTypeNumber  ValueType = "number"
	ValueTypeBoolean ValueType = "boolean"
	ValueTypeAny     ValueType = "any"
)
