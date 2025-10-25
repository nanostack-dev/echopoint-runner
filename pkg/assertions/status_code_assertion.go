package assertions

type StatusCodeAssertion struct {
	Operator string
	Expected int
}

func (a StatusCodeAssertion) Validate(_ interface{}) bool {
	return true
}

func (a StatusCodeAssertion) GetType() AssertionType {
	return AssertionTypeStatusCode
}
