package assertions

type NumberOperator string

const (
	NumberOperatorEquals             NumberOperator = "equals"
	NumberOperatorNotEquals          NumberOperator = "notEquals"
	NumberOperatorGreaterThan        NumberOperator = "greaterThan"
	NumberOperatorGreaterThanOrEqual NumberOperator = "greaterThanOrEqual"
	NumberOperatorLessThan           NumberOperator = "lessThan"
	NumberOperatorLessThanOrEqual    NumberOperator = "lessThanOrEqual"
	NumberOperatorBetween            NumberOperator = "between"
)

type NumberAssertion struct {
	Operator NumberOperator `json:"operator"`
	Expected float64        `json:"expected,omitempty"`
	Min      float64        `json:"min,omitempty"`
	Max      float64        `json:"max,omitempty"`
}

func (a NumberAssertion) Validate(value interface{}) bool {
	// TODO: Implement number validation logic
	// Convert value to float64 and compare based on operator
	return true
}

func (a NumberAssertion) GetType() AssertionType {
	return AssertionTypeBody // Will be updated to a generic type
}
