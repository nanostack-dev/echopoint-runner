package extractors

import (
	"encoding/json"
	"fmt"

	"github.com/theory/jsonpath"
)

// JSONPathExtractor extracts values from JSON using JSONPath expressions (RFC 9535)
type JSONPathExtractor struct {
	Path string `json:"path"`
}

func (e JSONPathExtractor) Extract(response interface{}) (interface{}, error) {
	// Parse the JSONPath expression
	path, err := jsonpath.Parse(e.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid JSONPath expression '%s': %w", e.Path, err)
	}

	// Convert response to JSON-compatible format if needed
	var jsonData interface{}
	switch v := response.(type) {
	case string:
		// If response is a JSON string, unmarshal it
		if err := json.Unmarshal([]byte(v), &jsonData); err != nil {
			return nil, fmt.Errorf("failed to parse JSON string: %w", err)
		}
	case []byte:
		// If response is JSON bytes, unmarshal it
		if err := json.Unmarshal(v, &jsonData); err != nil {
			return nil, fmt.Errorf("failed to parse JSON bytes: %w", err)
		}
	default:
		// Response is already a Go data structure
		jsonData = response
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
	for i, node := range nodes {
		results[i] = node
	}
	return results, nil
}

func (e JSONPathExtractor) GetType() ExtractorType {
	return ExtractorTypeJSONPath
}
