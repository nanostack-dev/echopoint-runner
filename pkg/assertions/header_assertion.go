package assertions

type HeaderAssertion struct {
	Operator string
	Expected interface{}
	Path     string
}

func (a HeaderAssertion) Validate(_ interface{}) bool {
	return true
}

func (a HeaderAssertion) GetType() AssertionType {
	return AssertionTypeHeader
}
