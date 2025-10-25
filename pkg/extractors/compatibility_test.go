package extractors

import (
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/operators"
	"github.com/stretchr/testify/assert"
)

func TestGetCompatibleOperators_StatusCode(t *testing.T) {
	ops := GetCompatibleOperators(ExtractorTypeStatusCode)

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
	ops := GetCompatibleOperators(ExtractorTypeHeader)

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
	ops := GetCompatibleOperators(ExtractorTypeJSONPath)

	// Should have both string and number operators (can extract any type)
	assert.Contains(t, ops, operators.OperatorTypeEquals)
	assert.Contains(t, ops, operators.OperatorTypeContains)
	assert.Contains(t, ops, operators.OperatorTypeGreaterThan)
	assert.Contains(t, ops, operators.OperatorTypeBetween)
}

func TestGetCompatibleOperators_XMLPath(t *testing.T) {
	ops := GetCompatibleOperators(ExtractorTypeXMLPath)

	// Should have both string and number operators (can extract any type)
	assert.Contains(t, ops, operators.OperatorTypeEquals)
	assert.Contains(t, ops, operators.OperatorTypeContains)
	assert.Contains(t, ops, operators.OperatorTypeGreaterThan)
	assert.Contains(t, ops, operators.OperatorTypeBetween)
}

func TestGetExtractorOutputType(t *testing.T) {
	testCases := []struct {
		extractor    ExtractorType
		expectedType string
	}{
		{ExtractorTypeStatusCode, "number"},
		{ExtractorTypeHeader, "string"},
		{ExtractorTypeJSONPath, "any"},
		{ExtractorTypeXMLPath, "any"},
	}

	for _, tc := range testCases {
		t.Run(
			string(tc.extractor), func(t *testing.T) {
				outputType := GetExtractorOutputType(tc.extractor)
				assert.Equal(t, tc.expectedType, outputType)
			},
		)
	}
}

func TestIsOperatorCompatible(t *testing.T) {
	testCases := []struct {
		name        string
		extractor   ExtractorType
		operator    operators.OperatorType
		shouldMatch bool
	}{
		{
			"StatusCode + Equals (valid)",
			ExtractorTypeStatusCode,
			operators.OperatorTypeEquals,
			true,
		},
		{
			"StatusCode + GreaterThan (valid)",
			ExtractorTypeStatusCode,
			operators.OperatorTypeGreaterThan,
			true,
		},
		{
			"StatusCode + Contains (invalid)",
			ExtractorTypeStatusCode,
			operators.OperatorTypeContains,
			false,
		},
		{
			"Header + Contains (valid)",
			ExtractorTypeHeader,
			operators.OperatorTypeContains,
			true,
		},
		{
			"Header + Regex (valid)",
			ExtractorTypeHeader,
			operators.OperatorTypeRegex,
			true,
		},
		{
			"Header + GreaterThan (invalid)",
			ExtractorTypeHeader,
			operators.OperatorTypeGreaterThan,
			false,
		},
		{
			"JSONPath + Contains (valid)",
			ExtractorTypeJSONPath,
			operators.OperatorTypeContains,
			true,
		},
		{
			"JSONPath + Between (valid)",
			ExtractorTypeJSONPath,
			operators.OperatorTypeBetween,
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				result := IsOperatorCompatible(tc.extractor, tc.operator)
				assert.Equal(t, tc.shouldMatch, result)
			},
		)
	}
}

func TestGetAllExtractorCompatibilities(t *testing.T) {
	all := GetAllExtractorCompatibilities()

	assert.Len(t, all, 4, "Should have 4 extractor types")

	// Verify each extractor has compatibility info
	extractorTypes := make(map[ExtractorType]bool)
	for _, compat := range all {
		extractorTypes[compat.ExtractorType] = true
		assert.NotEmpty(t, compat.CompatibleOperators, "Should have compatible operators")
		assert.NotEmpty(t, compat.OutputType, "Should have output type")
	}

	assert.True(t, extractorTypes[ExtractorTypeJSONPath])
	assert.True(t, extractorTypes[ExtractorTypeXMLPath])
	assert.True(t, extractorTypes[ExtractorTypeStatusCode])
	assert.True(t, extractorTypes[ExtractorTypeHeader])
}

func TestGetExtractorCompatibilityMap(t *testing.T) {
	compatMap := GetExtractorCompatibilityMap()

	assert.Len(t, compatMap, 4, "Should have 4 extractors")

	// Verify structure
	for extractorType, compat := range compatMap {
		assert.Equal(t, extractorType, compat.ExtractorType)
		assert.NotEmpty(t, compat.CompatibleOperators)
		assert.NotEmpty(t, compat.OutputType)
	}
}
