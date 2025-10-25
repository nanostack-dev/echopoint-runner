package node

// CompositeAssertion combines an extractor with an operator for validation
type CompositeAssertion struct {
	ExtractorType string      `json:"extractorType"` // jsonPath, xmlPath, statusCode, header
	ExtractorData interface{} `json:"extractorData"` // Configuration for the extractor
	OperatorType  string      `json:"operatorType"`  // equals, contains, greaterThan, etc.
	OperatorData  interface{} `json:"operatorData"`  // Configuration for the operator
}
