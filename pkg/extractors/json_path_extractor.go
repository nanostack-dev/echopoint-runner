package extractors

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/theory/jsonpath"
)

// JSONPathExtractor extracts values from JSON using JSONPath expressions (RFC 9535).
type JSONPathExtractor struct {
	Path string `json:"path"`
}

func (e JSONPathExtractor) Extract(ctx ResponseContext) (interface{}, error) {
	log.Debug().
		Str("extractorType", string(ExtractorTypeJSONPath)).
		Str("path", e.Path).
		Msg("Starting JSONPath extraction")

	// Parse the JSONPath expression
	path, err := jsonpath.Parse(e.Path)
	if err != nil {
		err = fmt.Errorf("invalid JSONPath expression '%s': %w", e.Path, err)
		log.Error().
			Str("path", e.Path).
			Err(err).
			Msg("JSONPath parsing failed")
		return nil, err
	}

	// Get parsed body from context using ParsedBodyReader interface
	var jsonData interface{}

	// Try to get parsed body from context
	pbr, ok := ctx.(ParsedBodyReader)
	if !ok {
		errParsedBody := errors.New("context does not support ParsedBodyReader interface")
		log.Error().
			Err(errParsedBody).
			Msg("ResponseContext does not support ParsedBodyReader")
		return nil, errParsedBody
	}

	jsonData = pbr.GetParsedBody()
	if jsonData == nil {
		log.Debug().
			Msg("Parsed body is nil, attempting manual JSON parse")
		// Fallback: try to parse raw body manually
		rawBody := pbr.GetRawBody()
		if unmarshalErr := json.Unmarshal(rawBody, &jsonData); unmarshalErr != nil {
			err = fmt.Errorf("failed to parse JSON from body: %w", unmarshalErr)
			log.Error().
				Err(err).
				Msg("JSON parsing failed")
			return nil, err
		}
	}

	// Execute the JSONPath query
	nodes := path.Select(jsonData)

	log.Debug().
		Str("path", e.Path).
		Int("matchCount", len(nodes)).
		Msg("JSONPath query executed")

	// Handle results
	if len(nodes) == 0 {
		jsonPathError := fmt.Errorf("JSONPath '%s' did not match any nodes", e.Path)
		log.Warn().
			Str("path", e.Path).
			Err(jsonPathError).
			Msg("JSONPath did not match any nodes")
		return nil, jsonPathError
	}

	// If single result, return the value directly
	if len(nodes) == 1 {
		log.Debug().
			Str("path", e.Path).
			Any("value", nodes[0]).
			Msg("JSONPath extraction succeeded with single result")
		return nodes[0], nil
	}

	// If multiple results, return as slice
	results := make([]interface{}, len(nodes))
	copy(results, nodes)
	log.Debug().
		Str("path", e.Path).
		Int("resultCount", len(results)).
		Msg("JSONPath extraction succeeded with multiple results")
	return results, nil
}

func (e JSONPathExtractor) GetType() ExtractorType {
	return ExtractorTypeJSONPath
}
