package assertions

type HeaderAssertion struct {
	Operator string
	Expected any
	Path     string
}

func (a HeaderAssertion) Validate(_ any) bool {
	return true
}

func (a HeaderAssertion) GetType() AssertionType {
	return AssertionTypeHeader
}
