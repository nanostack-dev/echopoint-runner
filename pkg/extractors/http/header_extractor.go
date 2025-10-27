package http

import (
	"errors"
	"fmt"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
)

// HeaderExtractor extracts HTTP header values from a response.
type HeaderExtractor struct {
	HeaderName string `json:"headerName"`
}

func (e HeaderExtractor) Extract(ctx extractors.ResponseContext) (interface{}, error) {
	// Use the HeaderAccessor interface to get the header value
	if ha, ok := ctx.(extractors.HeaderAccessor); ok {
		value := ha.GetHeader(e.HeaderName)
		if value != "" {
			return value, nil
		}
		return nil, fmt.Errorf("header %s not found", e.HeaderName)
	}

	return nil, errors.New("context does not implement HeaderAccessor interface")
}

func (e HeaderExtractor) GetType() extractors.ExtractorType {
	return extractors.ExtractorTypeHeader
}
