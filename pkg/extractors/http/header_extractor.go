package httpextractors

import (
	"errors"
	"fmt"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// HeaderExtractor extracts HTTP header values from a response.
type HeaderExtractor struct {
	HeaderName string `json:"header_name"`
}

func (e HeaderExtractor) Extract(ctx extractors.ResponseContext) (any, error) {
	log.Debug().
		Str("extractorType", string(spi.ExtractorTypeHeader)).
		Str("headerName", e.HeaderName).
		Msg("Starting header extraction")

	if ha, ok := ctx.(extractors.HeaderAccessor); ok {
		if value, found := lookupHeader(ha.Headers(), e.HeaderName); found {
			log.Debug().
				Str("extractorType", string(spi.ExtractorTypeHeader)).
				Str("headerName", e.HeaderName).
				Msg("Header extracted successfully")
			return value, nil
		}
		err := fmt.Errorf("header %s not found", e.HeaderName)
		return nil, err
	}

	err := errors.New("context does not implement HeaderAccessor interface")
	return nil, err
}

// lookupHeader returns the first value of a header and whether it is present,
// so a header sent with an empty value extracts as "" instead of not found.
func lookupHeader(headers http.Header, name string) (string, bool) {
	values, found := headers[textproto.CanonicalMIMEHeaderKey(name)]
	if !found {
		for key, candidate := range headers {
			if strings.EqualFold(key, name) {
				values, found = candidate, true
				break
			}
		}
	}
	if !found {
		return "", false
	}
	if len(values) == 0 {
		return "", true
	}
	return values[0], true
}

func (e HeaderExtractor) GetType() spi.ExtractorType {
	return spi.ExtractorTypeHeader
}
