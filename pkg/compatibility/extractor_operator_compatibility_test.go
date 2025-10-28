package compatibility_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/compatibility"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/operators"
	"github.com/stretchr/testify/assert"
)

func TestGetCompatibleOperators_StatusCode(t *testing.T) {
	ops := compatibility.GetCompatibleOperators(extractors.ExtractorTypeStatusCode)

	// Should have number operators
	assert.Contains(t, ops, operators.OperatorTypeEquals)
	assert.Contains(t, ops, operators.OperatorTypeGreaterThan)
	assert.Contains(t, ops, operators.OperatorTypeBetween)

	// Should NOT have string operators
	assert.NotContains(t, ops, operators.OperatorTypeContains)
	assert.NotContains(t, ops, operators.OperatorTypeStartsWith)
	assert.NotContains(t, ops, operators.OperatorTypeRegex)
}

func TestGetCompatibleOperators_Header(t *testing.T) {
	ops := compatibility.GetCompatibleOperators(extractors.ExtractorTypeHeader)

	// Should have string operators
	assert.Contains(t, ops, operators.OperatorTypeEquals)
	assert.Contains(t, ops, operators.OperatorTypeContains)
	assert.Contains(t, ops, operators.OperatorTypeStartsWith)
	assert.Contains(t, ops, operators.OperatorTypeRegex)

	// Should NOT have number operators
	assert.NotContains(t, ops, operators.OperatorTypeGreaterThan)
	assert.NotContains(t, ops, operators.OperatorTypeBetween)
}

func TestGetCompatibleOperators_JSONPath(t *testing.T) {
	ops := compatibility.GetCompatibleOperators(extractors.ExtractorTypeJSONPath)

	// Should have both string and number operators (can extract any type)
	assert.Contains(t, ops, operators.OperatorTypeEquals)
	assert.Contains(t, ops, operators.OperatorTypeContains)
	assert.Contains(t, ops, operators.OperatorTypeGreaterThan)
	assert.Contains(t, ops, operators.OperatorTypeBetween)
}

func TestGetCompatibleOperators_XMLPath(t *testing.T) {
	ops := compatibility.GetCompatibleOperators(extractors.ExtractorTypeXMLPath)

	// Should have both string and number operators (can extract any type)
	assert.Contains(t, ops, operators.OperatorTypeEquals)
	assert.Contains(t, ops, operators.OperatorTypeContains)
	assert.Contains(t, ops, operators.OperatorTypeGreaterThan)
	assert.Contains(t, ops, operators.OperatorTypeBetween)
}

func TestGetExtractorOutputType(t *testing.T) {
	testCases := []struct {
		extractor    extractors.ExtractorType
		expectedType string
	}{
		{extractors.ExtractorTypeStatusCode, "number"},
		{extractors.ExtractorTypeHeader, "string"},
		{extractors.ExtractorTypeJSONPath, "any"},
		{extractors.ExtractorTypeXMLPath, "any"},
	}

	for _, tc := range testCases {
		t.Run(
			string(tc.extractor), func(t *testing.T) {
				outputType := compatibility.GetExtractorOutputType(tc.extractor)
				assert.Equal(t, tc.expectedType, outputType)
			},
		)
	}
}

func TestIsOperatorCompatible(t *testing.T) {
	testCases := []struct {
		name        string
		extractor   extractors.ExtractorType
		operator    operators.OperatorType
		shouldMatch bool
	}{
		{
			"StatusCode + Equals (valid)",
			extractors.ExtractorTypeStatusCode,
			operators.OperatorTypeEquals,
			true,
		},
		{
			"StatusCode + GreaterThan (valid)",
			extractors.ExtractorTypeStatusCode,
			operators.OperatorTypeGreaterThan,
			true,
		},
		{
			"StatusCode + Contains (invalid)",
			extractors.ExtractorTypeStatusCode,
			operators.OperatorTypeContains,
			false,
		},
		{
			"Header + Contains (valid)",
			extractors.ExtractorTypeHeader,
			operators.OperatorTypeContains,
			true,
		},
		{
			"Header + Regex (valid)",
			extractors.ExtractorTypeHeader,
			operators.OperatorTypeRegex,
			true,
		},
		{
			"Header + GreaterThan (invalid)",
			extractors.ExtractorTypeHeader,
			operators.OperatorTypeGreaterThan,
			false,
		},
		{
			"JSONPath + Contains (valid)",
			extractors.ExtractorTypeJSONPath,
			operators.OperatorTypeContains,
			true,
		},
		{
			"JSONPath + Between (valid)",
			extractors.ExtractorTypeJSONPath,
			operators.OperatorTypeBetween,
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				result := compatibility.IsOperatorCompatible(tc.extractor, tc.operator)
				assert.Equal(t, tc.shouldMatch, result)
			},
		)
	}
}

func TestGetAllExtractorCompatibilities(t *testing.T) {
	all := compatibility.GetAllExtractorCompatibilities()

	assert.Len(t, all, 5, "Should have 5 extractor types")

	// Verify each extractor has compatibility info
	extractorTypes := make(map[extractors.ExtractorType]bool)
	for _, compat := range all {
		extractorTypes[compat.ExtractorType] = true
		assert.NotEmpty(t, compat.CompatibleOperators, "Should have compatible operators")
		assert.NotEmpty(t, compat.OutputType, "Should have output type")
	}

	assert.True(t, extractorTypes[extractors.ExtractorTypeJSONPath])
	assert.True(t, extractorTypes[extractors.ExtractorTypeXMLPath])
	assert.True(t, extractorTypes[extractors.ExtractorTypeStatusCode])
	assert.True(t, extractorTypes[extractors.ExtractorTypeHeader])
	assert.True(t, extractorTypes[extractors.ExtractorTypeBody])
}

func TestGetExtractorCompatibilityMap(t *testing.T) {
	compatMap := compatibility.GetExtractorCompatibilityMap()

	assert.Len(t, compatMap, 5, "Should have 5 extractors")

	// Verify structure
	for extractorType, compat := range compatMap {
		assert.Equal(t, extractorType, compat.ExtractorType)
		assert.NotEmpty(t, compat.CompatibleOperators)
		assert.NotEmpty(t, compat.OutputType)
	}
}
