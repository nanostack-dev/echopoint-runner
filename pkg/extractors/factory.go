package extractors

import (
	"encoding/json"
	"fmt"
	"sync"
)

//nolint:gochecknoglobals
var (
	extractorRegistry = make(map[ExtractorType]func([]byte) (AnyExtractor, error))
	registryMutex     sync.RWMutex
)

// RegisterExtractor registers a factory function for an extractor type.
func RegisterExtractor(extType ExtractorType, factory func([]byte) (AnyExtractor, error)) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	extractorRegistry[extType] = factory
}

// UnmarshalExtractor creates an appropriate Extractor from raw JSON.
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

	case ExtractorTypeStatusCode, ExtractorTypeHeader:
		// These are registered in the http package init()
		registryMutex.RLock()
		factory, ok := extractorRegistry[peek.Type]
		registryMutex.RUnlock()
		if ok {
			return factory(data)
		}
		return nil, fmt.Errorf("extractor type %s not registered", peek.Type)

	default:
		return nil, fmt.Errorf("unknown extractor type: %s", peek.Type)
	}
}
