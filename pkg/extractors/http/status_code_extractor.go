package http

import (
	"errors"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
)

// StatusCodeExtractor extracts the HTTP status code from a response.
type StatusCodeExtractor struct{}

func (e StatusCodeExtractor) Extract(ctx extractors.ResponseContext) (interface{}, error) {
	log.Debug().
		Str("extractorType", string(extractors.ExtractorTypeStatusCode)).
		Msg("Starting status code extraction")

	// Use the StatusReader interface to get the status code
	if sr, ok := ctx.(extractors.StatusReader); ok {
		status := sr.GetStatus()
		log.Debug().
			Str("extractorType", string(extractors.ExtractorTypeStatusCode)).
			Int("statusCode", status).
			Msg("Status code extracted successfully")
		return status, nil
	}

	err := errors.New("context does not implement StatusReader interface")
	log.Error().
		Str("extractorType", string(extractors.ExtractorTypeStatusCode)).
		Err(err).
		Msg("Failed to extract status code")
	return nil, err
}

func (e StatusCodeExtractor) GetType() extractors.ExtractorType {
	return extractors.ExtractorTypeStatusCode
}
