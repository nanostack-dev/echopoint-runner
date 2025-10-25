package examples

import (
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/assertions"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
)

// ExampleStringAssertion demonstrates basic string assertion usage
func ExampleStringAssertion() {
	// Simple equality check
	equalsAssertion := assertions.StringAssertion{
		Operator: assertions.StringOperatorEquals,
		Expected: "success",
	}

	// Contains check
	containsAssertion := assertions.StringAssertion{
		Operator: assertions.StringOperatorContains,
		Expected: "error",
	}

	// Regex pattern matching
	regexAssertion := assertions.StringAssertion{
		Operator: assertions.StringOperatorRegex,
		Expected: "^[A-Z]{3}-\\d{4}$", // e.g., ABC-1234
	}

	// Empty check (no Expected value needed)
	emptyAssertion := assertions.StringAssertion{
		Operator: assertions.StringOperatorEmpty,
	}

	// Use the assertions
	_ = equalsAssertion
	_ = containsAssertion
	_ = regexAssertion
	_ = emptyAssertion
}

// ExampleNumberAssertion demonstrates number assertion usage
func ExampleNumberAssertion() {
	// Equality check
	equalsAssertion := assertions.NumberAssertion{
		Operator: assertions.NumberOperatorEquals,
		Expected: 200,
	}

	// Greater than check
	greaterThanAssertion := assertions.NumberAssertion{
		Operator: assertions.NumberOperatorGreaterThan,
		Expected: 0,
	}

	// Between range check
	betweenAssertion := assertions.NumberAssertion{
		Operator: assertions.NumberOperatorBetween,
		Min:      1,
		Max:      100,
	}

	// Use the assertions
	_ = equalsAssertion
	_ = greaterThanAssertion
	_ = betweenAssertion
}

// ExampleBooleanAssertion demonstrates boolean assertion usage
func ExampleBooleanAssertion() {
	// Simple true/false check
	trueAssertion := assertions.BooleanAssertion{
		Expected: true,
	}

	falseAssertion := assertions.BooleanAssertion{
		Expected: false,
	}

	// Use the assertions
	_ = trueAssertion
	_ = falseAssertion
}

// ExampleJSONPathExtraction demonstrates extracting values with JSONPath
func ExampleJSONPathExtraction() {
	// Extract user name from JSON response
	nameExtractor := extractors.JSONPathExtractor{
		Path: "$.user.name",
	}

	// Extract email from JSON response
	emailExtractor := extractors.JSONPathExtractor{
		Path: "$.user.email",
	}

	// Extract nested array element
	itemExtractor := extractors.JSONPathExtractor{
		Path: "$.orders[0].id",
	}

	// Use the extractors
	_ = nameExtractor
	_ = emailExtractor
	_ = itemExtractor
}

// ExampleXMLPathExtraction demonstrates extracting values with XMLPath
func ExampleXMLPathExtraction() {
	// Extract status from XML response
	statusExtractor := extractors.XMLPathExtractor{
		Path: "/response/status",
	}

	// Extract user name from XML
	nameExtractor := extractors.XMLPathExtractor{
		Path: "/response/user/name",
	}

	// Use the extractors
	_ = statusExtractor
	_ = nameExtractor
}

// ExampleCompositeAssertion demonstrates combining extractors with assertions
func ExampleCompositeAssertion() {
	// Extract user name with JSONPath and validate it equals "John Doe"
	// This would be used in the JSON flow definition like:
	// {
	//   "extractorType": "jsonPath",
	//   "extractorData": {"path": "$.user.name"},
	//   "assertionType": "string",
	//   "assertionData": {"operator": "equals", "expected": "John Doe"}
	// }

	// Extract status code and validate it equals 200
	// {
	//   "extractorType": "statusCode",
	//   "extractorData": {},
	//   "assertionType": "number",
	//   "assertionData": {"operator": "equals", "expected": 200}
	// }

	// Extract header value and validate it contains "application/json"
	// {
	//   "extractorType": "header",
	//   "extractorData": {"headerName": "Content-Type"},
	//   "assertionType": "string",
	//   "assertionData": {"operator": "contains", "expected": "application/json"}
	// }
}
