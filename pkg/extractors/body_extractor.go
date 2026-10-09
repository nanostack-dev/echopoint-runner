package extractors

import (
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/rs/zerolog/log"
)

// BodyExtractor extracts the entire response body.
// It can be used to capture the complete response as a value.
type BodyExtractor struct {
	// No additional fields needed for extracting the full body
}

func (e BodyExtractor) Extract(ctx ResponseContext) (any, error) {
	log.Debug().
		Str("extractorType", string(spi.ExtractorTypeBody)).
		Msg("Starting body extraction")

	// Try to get parsed body first (most common case)
	if pbr, ok := ctx.(ParsedBodyReader); ok {
		body := pbr.GetParsedBody()
		log.Debug().
			Str("extractorType", string(spi.ExtractorTypeBody)).
			Msg("Body extracted successfully")
		return body, nil
	}

	// If no parsed body, return nil with error
	return nil, ErrNotImplemented
}

// ExtractText returns the body as received when it parsed into a JSON object or
// array, so a string operator matches the wire text instead of a Go map printout.
// A scalar body keeps its parsed value.
func (e BodyExtractor) ExtractText(ctx ResponseContext) (any, error) {
	body, err := e.Extract(ctx)
	if err != nil {
		return nil, err
	}
	switch body.(type) {
	case map[string]any, []any:
	default:
		return body, nil
	}
	if pbr, isReader := ctx.(ParsedBodyReader); isReader && pbr.GetRawBody() != nil {
		return string(pbr.GetRawBody()), nil
	}
	return body, nil
}

func (e BodyExtractor) GetType() spi.ExtractorType {
	return spi.ExtractorTypeBody
}
