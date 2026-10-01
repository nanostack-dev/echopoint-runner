package operators

// OperatorType is the wire identifier for an assertion operator. Each value has
// one registered Comparator (see registry.go); the constants below are the
// built-ins.
type OperatorType string

const (
	OperatorTypeEquals             OperatorType = "equals"
	OperatorTypeNotEquals          OperatorType = "not_equals"
	OperatorTypeContains           OperatorType = "contains"
	OperatorTypeNotContains        OperatorType = "not_contains"
	OperatorTypeStartsWith         OperatorType = "starts_with"
	OperatorTypeEndsWith           OperatorType = "ends_with"
	OperatorTypeRegex              OperatorType = "regex"
	OperatorTypeEmpty              OperatorType = "empty"
	OperatorTypeNotEmpty           OperatorType = "not_empty"
	OperatorTypeGreaterThan        OperatorType = "greater_than"
	OperatorTypeLessThan           OperatorType = "less_than"
	OperatorTypeGreaterThanOrEqual OperatorType = "greater_than_or_equal"
	OperatorTypeLessThanOrEqual    OperatorType = "less_than_or_equal"
	OperatorTypeBetween            OperatorType = "between"
)
