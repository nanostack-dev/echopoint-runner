package extractors

import (
	"encoding/json"
	"fmt"

	"github.com/theory/jsonpath"
)

// JSONPathExtractor extracts values from JSON using JSONPath expressions (RFC 9535).
type JSONPathExtractor struct {
	Path string `json:"path"`
}

func (e JSONPathExtractor) Extract(ctx ResponseContext) (interface{}, error) {
	// Parse the JSONPath expression
	path, err := jsonpath.Parse(e.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid JSONPath expression '%s': %w", e.Path, err)
	}

	// Get parsed body from context using ParsedBodyReader interface
	var jsonData interface{}

	// Try to get parsed body first
	if pbr, ok := ctx.(ParsedBodyReader); ok {
		jsonData = pbr.GetParsedBody()
	} else {
		// Fallback: try to get raw body and parse it
		if rbr, ok := ctx.(ParsedBodyReader); ok {
			rawBody := rbr.GetRawBody()
			if unmarshalErr := json.Unmarshal(rawBody, &jsonData); unmarshalErr != nil {
				return nil, fmt.Errorf("failed to parse JSON from body: %w", unmarshalErr)
			}
		} else {
			return nil, fmt.Errorf("context does not support ParsedBodyReader interface")
		}
	}

	// Execute the JSONPath query
	nodes := path.Select(jsonData)

	// Handle results
	if len(nodes) == 0 {
		return nil, fmt.Errorf("JSONPath '%s' did not match any nodes", e.Path)
	}

	// If single result, return the value directly
	if len(nodes) == 1 {
		return nodes[0], nil
	}

	// If multiple results, return as slice
	results := make([]interface{}, len(nodes))
	copy(results, nodes)
	return results, nil
}

func (e JSONPathExtractor) GetType() ExtractorType {
	return ExtractorTypeJSONPath
}
