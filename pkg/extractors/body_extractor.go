package extractors

import (
	"github.com/rs/zerolog/log"
)

// BodyExtractor extracts the entire response body.
// It can be used to capture the complete response as a value.
type BodyExtractor struct {
	// No additional fields needed for extracting the full body
}

func (e BodyExtractor) Extract(ctx ResponseContext) (interface{}, error) {
	log.Debug().
		Str("extractorType", string(ExtractorTypeBody)).
		Msg("Starting body extraction")

	// Try to get parsed body first (most common case)
	if pbr, ok := ctx.(ParsedBodyReader); ok {
		body := pbr.GetParsedBody()
		log.Debug().
			Str("extractorType", string(ExtractorTypeBody)).
			Msg("Body extracted successfully")
		return body, nil
	}

	// If no parsed body, return nil with error
	log.Error().
		Str("extractorType", string(ExtractorTypeBody)).
		Err(ErrNotImplemented).
		Msg("Failed to extract body: ParsedBodyReader not supported")
	return nil, ErrNotImplemented
}

func (e BodyExtractor) GetType() ExtractorType {
	return ExtractorTypeBody
}
