package examples

import (
	"fmt"
	"strings"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/operators"
)

// PrintCompatibilityMatrix prints a human-readable compatibility matrix
func PrintCompatibilityMatrix() {
	fmt.Println("# Extractor-Operator Compatibility Matrix")
	fmt.Println()

	compatibilities := extractors.GetAllExtractorCompatibilities()

	for _, compat := range compatibilities {
		fmt.Printf("## %s (Output: %s)\n\n", compat.ExtractorType, compat.OutputType)
		fmt.Println("Compatible Operators:")

		// Group operators by category
		stringOps := []operators.OperatorType{}
		numberOps := []operators.OperatorType{}

		for _, op := range compat.CompatibleOperators {
			if isStringOperator(op) {
				stringOps = append(stringOps, op)
			} else if isNumberOperator(op) {
				numberOps = append(numberOps, op)
			}
		}

		if len(stringOps) > 0 {
			fmt.Println("\n**String Operators:**")
			for _, op := range stringOps {
				fmt.Printf("  - `%s`\n", op)
			}
		}

		if len(numberOps) > 0 {
			fmt.Println("\n**Number Operators:**")
			for _, op := range numberOps {
				fmt.Printf("  - `%s`\n", op)
			}
		}

		fmt.Println()
	}
}

// GenerateCompatibilityTable generates a markdown table showing compatibility
func GenerateCompatibilityTable() string {
	var sb strings.Builder

	sb.WriteString("| Extractor | Output Type | Compatible Operators |\n")
	sb.WriteString("|-----------|-------------|---------------------|\n")

	compatibilities := extractors.GetAllExtractorCompatibilities()

	for _, compat := range compatibilities {
		opsList := []string{}
		for _, op := range compat.CompatibleOperators {
			opsList = append(opsList, string(op))
		}

		sb.WriteString(
			fmt.Sprintf(
				"| %s | %s | %s |\n",
				compat.ExtractorType,
				compat.OutputType,
				strings.Join(opsList, ", "),
			),
		)
	}

	return sb.String()
}

// ValidateConfiguration validates an extractor-operator combination
func ValidateConfiguration(
	extractorType extractors.ExtractorType, operatorType operators.OperatorType,
) error {
	if !extractors.IsOperatorCompatible(extractorType, operatorType) {
		return fmt.Errorf(
			"operator '%s' is not compatible with extractor '%s' (output type: %s)",
			operatorType,
			extractorType,
			extractors.GetExtractorOutputType(extractorType),
		)
	}
	return nil
}

func isStringOperator(op operators.OperatorType) bool {
	stringOps := []operators.OperatorType{
		operators.OperatorTypeContains,
		operators.OperatorTypeNotContains,
		operators.OperatorTypeStartsWith,
		operators.OperatorTypeEndsWith,
		operators.OperatorTypeRegex,
		operators.OperatorTypeEmpty,
		operators.OperatorTypeNotEmpty,
	}

	for _, stringOp := range stringOps {
		if op == stringOp {
			return true
		}
	}
	return false
}

func isNumberOperator(op operators.OperatorType) bool {
	numberOps := []operators.OperatorType{
		operators.OperatorTypeGreaterThan,
		operators.OperatorTypeLessThan,
		operators.OperatorTypeGreaterThanOrEqual,
		operators.OperatorTypeLessThanOrEqual,
		operators.OperatorTypeBetween,
	}

	for _, numOp := range numberOps {
		if op == numOp {
			return true
		}
	}
	return false
}

// ExampleCompatibilityUsage demonstrates how to use the compatibility system
func ExampleCompatibilityUsage() {
	// Check if a combination is valid
	err := ValidateConfiguration(
		extractors.ExtractorTypeStatusCode,
		operators.OperatorTypeBetween,
	)
	if err != nil {
		fmt.Println("Invalid configuration:", err)
	} else {
		fmt.Println("Configuration is valid!")
	}

	// Invalid combination
	err = ValidateConfiguration(
		extractors.ExtractorTypeStatusCode,
		operators.OperatorTypeContains, // String operator on number extractor
	)
	if err != nil {
		fmt.Println("Invalid configuration:", err)
		// Output: Invalid configuration: operator 'contains' is not compatible with extractor 'statusCode' (output type: number)
	}

	// Get all compatible operators for an extractor
	ops := extractors.GetCompatibleOperators(extractors.ExtractorTypeHeader)
	fmt.Printf("Header extractor supports %d operators\n", len(ops))

	// Print the full matrix
	PrintCompatibilityMatrix()
}
