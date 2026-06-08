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

// UnmarshalExtractor creates an Extractor from raw JSON via the registry.
// Built-in extractors register in builtin.go; statusCode/header in the http
// package — so this is a pure lookup with no per-type switch to edit.
func UnmarshalExtractor(data []byte) (AnyExtractor, error) {
	var peek struct {
		Type ExtractorType `json:"type"`
	}

	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("failed to peek extractor type: %w", err)
	}

	registryMutex.RLock()
	factory, ok := extractorRegistry[peek.Type]
	registryMutex.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown extractor type: %s", peek.Type)
	}
	return factory(data)
}
