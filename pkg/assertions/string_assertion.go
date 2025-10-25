package assertions

type StringOperator string

const (
	StringOperatorEquals      StringOperator = "equals"
	StringOperatorContains    StringOperator = "contains"
	StringOperatorStartsWith  StringOperator = "startsWith"
	StringOperatorEndsWith    StringOperator = "endsWith"
	StringOperatorRegex       StringOperator = "regex"
	StringOperatorNotEquals   StringOperator = "notEquals"
	StringOperatorNotContains StringOperator = "notContains"
	StringOperatorEmpty       StringOperator = "empty"
	StringOperatorNotEmpty    StringOperator = "notEmpty"
)

type StringAssertion struct {
	Operator StringOperator `json:"operator"`
	Expected string         `json:"expected,omitempty"`
}

func (a StringAssertion) Validate(_ interface{}) bool {
	// TODO: Implement string validation logic
	return true
}

func (a StringAssertion) GetType() AssertionType {
	return AssertionTypeBody
}
