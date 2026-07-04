package httpextractors

import (
	"errors"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// StatusCodeExtractor extracts the HTTP status code from a response.
type StatusCodeExtractor struct{}

func (e StatusCodeExtractor) Extract(ctx extractors.ResponseContext) (any, error) {
	log.Debug().
		Str("extractorType", string(spi.ExtractorTypeStatusCode)).
		Msg("Starting status code extraction")

	// Use the StatusReader interface to get the status code
	if sr, ok := ctx.(extractors.StatusReader); ok {
		status := sr.GetStatus()
		log.Debug().
			Str("extractorType", string(spi.ExtractorTypeStatusCode)).
			Int("statusCode", status).
			Msg("Status code extracted successfully")
		return status, nil
	}

	err := errors.New("context does not implement StatusReader interface")
	log.Error().
		Str("extractorType", string(spi.ExtractorTypeStatusCode)).
		Err(err).
		Msg("Failed to extract status code")
	return nil, err
}

func (e StatusCodeExtractor) GetType() spi.ExtractorType {
	return spi.ExtractorTypeStatusCode
}
