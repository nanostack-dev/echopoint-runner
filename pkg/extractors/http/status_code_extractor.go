package http

import (
	"errors"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
)

// StatusCodeExtractor extracts the HTTP status code from a response.
type StatusCodeExtractor struct{}

func (e StatusCodeExtractor) Extract(ctx extractors.ResponseContext) (interface{}, error) {
	// Use the StatusReader interface to get the status code
	if sr, ok := ctx.(extractors.StatusReader); ok {
		return sr.GetStatus(), nil
	}

	return nil, errors.New("context does not implement StatusReader interface")
}

func (e StatusCodeExtractor) GetType() extractors.ExtractorType {
	return extractors.ExtractorTypeStatusCode
}
