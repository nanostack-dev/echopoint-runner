package httpextractors

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
)

// HeaderExtractor extracts HTTP header values from a response.
type HeaderExtractor struct {
	HeaderName string `json:"headerName"`
}

func (e HeaderExtractor) Extract(ctx extractors.ResponseContext) (interface{}, error) {
	log.Debug().
		Str("extractorType", string(extractors.ExtractorTypeHeader)).
		Str("headerName", e.HeaderName).
		Msg("Starting header extraction")

	// Use the HeaderAccessor interface to get the header value
	if ha, ok := ctx.(extractors.HeaderAccessor); ok {
		value := ha.GetHeader(e.HeaderName)
		if value != "" {
			log.Debug().
				Str("extractorType", string(extractors.ExtractorTypeHeader)).
				Str("headerName", e.HeaderName).
				Str("value", value).
				Msg("Header extracted successfully")
			return value, nil
		}
		err := fmt.Errorf("header %s not found", e.HeaderName)
		log.Warn().
			Str("extractorType", string(extractors.ExtractorTypeHeader)).
			Str("headerName", e.HeaderName).
			Err(err).
			Msg("Header not found")
		return nil, err
	}

	err := errors.New("context does not implement HeaderAccessor interface")
	log.Error().
		Str("extractorType", string(extractors.ExtractorTypeHeader)).
		Str("headerName", e.HeaderName).
		Err(err).
		Msg("Failed to extract header")
	return nil, err
}

func (e HeaderExtractor) GetType() extractors.ExtractorType {
	return extractors.ExtractorTypeHeader
}
