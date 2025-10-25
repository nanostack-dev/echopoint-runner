package extractors

import "github.com/nanostack-dev/echopoint-flow-engine/pkg/operators"

// ExtractorOperatorCompatibility defines which operators are compatible with each extractor
type ExtractorOperatorCompatibility struct {
	ExtractorType       ExtractorType
	CompatibleOperators []operators.OperatorType
	OutputType          string // "string", "number", "boolean", "any"
}

// GetCompatibleOperators returns the list of operators compatible with an extractor
func GetCompatibleOperators(extractorType ExtractorType) []operators.OperatorType {
	compatibility := GetExtractorCompatibilityMap()
	if compat, ok := compatibility[extractorType]; ok {
		return compat.CompatibleOperators
	}
	return []operators.OperatorType{}
}

// GetExtractorOutputType returns the output type of an extractor
func GetExtractorOutputType(extractorType ExtractorType) string {
	compatibility := GetExtractorCompatibilityMap()
	if compat, ok := compatibility[extractorType]; ok {
		return compat.OutputType
	}
	return "any"
}

// IsOperatorCompatible checks if an operator is compatible with an extractor
func IsOperatorCompatible(extractorType ExtractorType, operatorType operators.OperatorType) bool {
	compatibleOps := GetCompatibleOperators(extractorType)
	for _, op := range compatibleOps {
		if op == operatorType {
			return true
		}
	}
	return false
}

// GetExtractorCompatibilityMap returns the complete compatibility mapping
func GetExtractorCompatibilityMap() map[ExtractorType]ExtractorOperatorCompatibility {
	return map[ExtractorType]ExtractorOperatorCompatibility{
		ExtractorTypeJSONPath: {
			ExtractorType: ExtractorTypeJSONPath,
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
		ExtractorTypeXMLPath: {
			ExtractorType: ExtractorTypeXMLPath,
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
		ExtractorTypeStatusCode: {
			ExtractorType: ExtractorTypeStatusCode,
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
		ExtractorTypeHeader: {
			ExtractorType: ExtractorTypeHeader,
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
	}
}

// GetAllExtractorCompatibilities returns all extractor compatibilities for documentation
func GetAllExtractorCompatibilities() []ExtractorOperatorCompatibility {
	compatMap := GetExtractorCompatibilityMap()
	result := make([]ExtractorOperatorCompatibility, 0, len(compatMap))
	for _, compat := range compatMap {
		result = append(result, compat)
	}
	return result
}
