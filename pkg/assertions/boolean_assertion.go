package assertions

type BooleanAssertion struct {
	Expected bool `json:"expected"`
}

func (a BooleanAssertion) Validate(_ interface{}) bool {
	// TODO: Implement boolean validation logic
	// Convert value to bool and compare with expected
	return true
}

func (a BooleanAssertion) GetType() AssertionType {
	return AssertionTypeBody // Will be updated to a generic type
}
