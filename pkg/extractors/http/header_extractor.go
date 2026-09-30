package httpextractors

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// HeaderExtractor extracts HTTP header values from a response.
type HeaderExtractor struct {
	HeaderName string `json:"headerName"`
}

// UnmarshalJSON reads header_name, the name the echopoint contract documents,
// as well as headerName, which older flows store.
func (e *HeaderExtractor) UnmarshalJSON(data []byte) error {
	var wire struct {
		HeaderName      string `json:"headerName"`
		HeaderNameSnake string `json:"header_name"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	e.HeaderName = cmp.Or(wire.HeaderNameSnake, wire.HeaderName)
	return nil
}

func (e HeaderExtractor) Extract(ctx extractors.ResponseContext) (any, error) {
	log.Debug().
		Str("extractorType", string(spi.ExtractorTypeHeader)).
		Str("headerName", e.HeaderName).
		Msg("Starting header extraction")

	// Use the HeaderAccessor interface to get the header value
	if ha, ok := ctx.(extractors.HeaderAccessor); ok {
		value := ha.GetHeader(e.HeaderName)
		if value != "" {
			log.Debug().
				Str("extractorType", string(spi.ExtractorTypeHeader)).
				Str("headerName", e.HeaderName).
				Msg("Header extracted successfully")
			return value, nil
		}
		err := fmt.Errorf("header %s not found", e.HeaderName)
		log.Warn().
			Str("extractorType", string(spi.ExtractorTypeHeader)).
			Str("headerName", e.HeaderName).
			Err(err).
			Msg("Header not found")
		return nil, err
	}

	err := errors.New("context does not implement HeaderAccessor interface")
	log.Error().
		Str("extractorType", string(spi.ExtractorTypeHeader)).
		Str("headerName", e.HeaderName).
		Err(err).
		Msg("Failed to extract header")
	return nil, err
}

func (e HeaderExtractor) GetType() spi.ExtractorType {
	return spi.ExtractorTypeHeader
}
