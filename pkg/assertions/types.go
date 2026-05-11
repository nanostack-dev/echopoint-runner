package assertions

type AssertionType string

const (
	AssertionTypeStatusCode AssertionType = "statusCode"
	AssertionTypeBody       AssertionType = "body"
	AssertionTypeHeader     AssertionType = "header"
)

type Assertion interface {
	Validate(response interface{}) bool
	GetType() AssertionType
}
