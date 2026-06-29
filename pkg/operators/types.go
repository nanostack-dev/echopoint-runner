package operators

// OperatorType is the wire identifier for an assertion operator. Each value has
// one registered Comparator (see registry.go); the constants below are the
// built-ins.
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
