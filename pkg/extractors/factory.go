package extractors

import (
	"encoding/json"
	"fmt"
)

// extractorRegistry holds factory functions for different extractor types
// This allows other packages to register their extractors without circular imports
var extractorRegistry = make(map[ExtractorType]func([]byte) (AnyExtractor, error))

// RegisterExtractor registers a factory function for an extractor type
func RegisterExtractor(extType ExtractorType, factory func([]byte) (AnyExtractor, error)) {
	extractorRegistry[extType] = factory
}

// UnmarshalExtractor creates an appropriate Extractor from raw JSON
func UnmarshalExtractor(data []byte) (AnyExtractor, error) {
	var peek struct {
		Type ExtractorType `json:"type"`
	}

	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("failed to peek extractor type: %w", err)
	}

	switch peek.Type {
	case ExtractorTypeJSONPath:
		var extractor JSONPathExtractor
		if err := json.Unmarshal(data, &extractor); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSONPath extractor: %w", err)
		}
		return extractor, nil

	case ExtractorTypeXMLPath:
		var extractor XMLPathExtractor
		if err := json.Unmarshal(data, &extractor); err != nil {
			return nil, fmt.Errorf("failed to unmarshal XMLPath extractor: %w", err)
		}
		return extractor, nil

	case ExtractorTypeBody:
		var extractor BodyExtractor
		if err := json.Unmarshal(data, &extractor); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Body extractor: %w", err)
		}
		return extractor, nil

	default:
		// Check if extractor is registered (e.g., from http package)
		if factory, ok := extractorRegistry[peek.Type]; ok {
			return factory(data)
		}
		return nil, fmt.Errorf("unknown extractor type: %s", peek.Type)
	}
}
