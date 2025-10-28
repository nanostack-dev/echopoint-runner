package compatibility

import (
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/operators"
)

// ExtractorOperatorCompatibility defines which operators are compatible with each extractor.
type ExtractorOperatorCompatibility struct {
	ExtractorType       extractors.ExtractorType
	CompatibleOperators []operators.OperatorType
	OutputType          string
}

// GetCompatibleOperators returns the list of operators compatible with an extractor.
func GetCompatibleOperators(extractorType extractors.ExtractorType) []operators.OperatorType {
	compatibility := GetExtractorCompatibilityMap()
	if compat, ok := compatibility[extractorType]; ok {
		return compat.CompatibleOperators
	}
	return []operators.OperatorType{}
}

// GetExtractorOutputType returns the output type of an extractor.
func GetExtractorOutputType(extractorType extractors.ExtractorType) string {
	compatibility := GetExtractorCompatibilityMap()
	if compat, ok := compatibility[extractorType]; ok {
		return compat.OutputType
	}
	return "any"
}

// IsOperatorCompatible checks if an operator is compatible with an extractor.
func IsOperatorCompatible(
	extractorType extractors.ExtractorType, operatorType operators.OperatorType,
) bool {
	compatibleOps := GetCompatibleOperators(extractorType)
	for _, op := range compatibleOps {
		if op == operatorType {
			return true
		}
	}
	return false
}

// GetExtractorCompatibilityMap returns the complete compatibility mapping.
func GetExtractorCompatibilityMap() map[extractors.ExtractorType]ExtractorOperatorCompatibility {
	return map[extractors.ExtractorType]ExtractorOperatorCompatibility{
		extractors.ExtractorTypeJSONPath: {
			ExtractorType: extractors.ExtractorTypeJSONPath,
			OutputType:    "any", // Can extract any type from JSON
			CompatibleOperators: []operators.OperatorType{
				// String operators
				operators.OperatorTypeEquals,
				operators.OperatorTypeNotEquals,
				operators.OperatorTypeContains,
				operators.OperatorTypeNotContains,
				operators.OperatorTypeStartsWith,
				operators.OperatorTypeEndsWith,
				operators.OperatorTypeRegex,
				operators.OperatorTypeEmpty,
				operators.OperatorTypeNotEmpty,
				// Number operators
				operators.OperatorTypeGreaterThan,
				operators.OperatorTypeLessThan,
				operators.OperatorTypeGreaterThanOrEqual,
				operators.OperatorTypeLessThanOrEqual,
				operators.OperatorTypeBetween,
			},
		},
		extractors.ExtractorTypeXMLPath: {
			ExtractorType: extractors.ExtractorTypeXMLPath,
			OutputType:    "any", // Can extract any type from XML
			CompatibleOperators: []operators.OperatorType{
				// String operators
				operators.OperatorTypeEquals,
				operators.OperatorTypeNotEquals,
				operators.OperatorTypeContains,
				operators.OperatorTypeNotContains,
				operators.OperatorTypeStartsWith,
				operators.OperatorTypeEndsWith,
				operators.OperatorTypeRegex,
				operators.OperatorTypeEmpty,
				operators.OperatorTypeNotEmpty,
				// Number operators
				operators.OperatorTypeGreaterThan,
				operators.OperatorTypeLessThan,
				operators.OperatorTypeGreaterThanOrEqual,
				operators.OperatorTypeLessThanOrEqual,
				operators.OperatorTypeBetween,
			},
		},
		extractors.ExtractorTypeStatusCode: {
			ExtractorType: extractors.ExtractorTypeStatusCode,
			OutputType:    "number",
			CompatibleOperators: []operators.OperatorType{
				// Number operators only
				operators.OperatorTypeEquals,
				operators.OperatorTypeNotEquals,
				operators.OperatorTypeGreaterThan,
				operators.OperatorTypeLessThan,
				operators.OperatorTypeGreaterThanOrEqual,
				operators.OperatorTypeLessThanOrEqual,
				operators.OperatorTypeBetween,
			},
		},
		extractors.ExtractorTypeHeader: {
			ExtractorType: extractors.ExtractorTypeHeader,
			OutputType:    "string",
			CompatibleOperators: []operators.OperatorType{
				// String operators only
				operators.OperatorTypeEquals,
				operators.OperatorTypeNotEquals,
				operators.OperatorTypeContains,
				operators.OperatorTypeNotContains,
				operators.OperatorTypeStartsWith,
				operators.OperatorTypeEndsWith,
				operators.OperatorTypeRegex,
				operators.OperatorTypeEmpty,
				operators.OperatorTypeNotEmpty,
			},
		},
		extractors.ExtractorTypeBody: {
			ExtractorType: extractors.ExtractorTypeBody,
			OutputType:    "any", // Can be any type (parsed JSON, XML, string, etc.)
			CompatibleOperators: []operators.OperatorType{
				// Any/body operators - body is captured as-is
				operators.OperatorTypeNotEmpty,
				operators.OperatorTypeEmpty,
			},
		},
	}
}

// GetAllExtractorCompatibilities returns all extractor compatibilities for documentation.
func GetAllExtractorCompatibilities() []ExtractorOperatorCompatibility {
	compatMap := GetExtractorCompatibilityMap()
	result := make([]ExtractorOperatorCompatibility, 0, len(compatMap))
	for _, compat := range compatMap {
		result = append(result, compat)
	}
	return result
}
