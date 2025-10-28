package node

import (
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	_ "github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors/http" // Register HTTP extractors in init()
)

// CompositeAssertion combines an extractor with an operator for validation.
type CompositeAssertion struct {
	Extractor     extractors.AnyExtractor `json:"-"`             // The actual extractor instance
	Operator      interface{}             `json:"-"`             // The actual operator instance (for future use)
	ExtractorType string                  `json:"extractorType"` // Legacy: jsonPath, xmlPath, statusCode, header
	ExtractorData interface{}             `json:"extractorData"` // Legacy: Configuration for the extractor
	OperatorType  string                  `json:"operatorType"`  // equals, contains, greaterThan, etc.
	OperatorData  interface{}             `json:"operatorData"`  // Configuration for the operator
}

// UnmarshalJSON implements custom unmarshaling for CompositeAssertion.
func (ca *CompositeAssertion) UnmarshalJSON(data []byte) error {
	type Alias CompositeAssertion
	aux := &struct {
		*Alias

		Extractor json.RawMessage `json:"extractor"`
		Operator  json.RawMessage `json:"operator"`
	}{
		Alias: (*Alias)(ca),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Unmarshal extractor if provided
	if len(aux.Extractor) > 0 {
		extractor, err := extractors.UnmarshalExtractor(aux.Extractor)
		if err != nil {
			return fmt.Errorf("failed to unmarshal assertion extractor: %w", err)
		}
		ca.Extractor = extractor
	}

	// TODO: Implement operator unmarshaling when needed
	// For now, just store the raw operator data
	if len(aux.Operator) > 0 {
		var opData interface{}
		if err := json.Unmarshal(aux.Operator, &opData); err != nil {
			return fmt.Errorf("failed to unmarshal assertion operator: %w", err)
		}
		ca.Operator = opData
	}

	return nil
}
