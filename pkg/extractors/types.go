package extractors

import (
	"errors"
	"io"
	"net/http"
)

type AnyExtractor interface {
	Extract(ctx ResponseContext) (interface{}, error)
	GetType() ExtractorType
}

type ExtractorType string

const (
	ExtractorTypeJSONPath   ExtractorType = "jsonPath"
	ExtractorTypeXMLPath    ExtractorType = "xmlPath"
	ExtractorTypeStatusCode ExtractorType = "statusCode"
	ExtractorTypeHeader     ExtractorType = "header"
	ExtractorTypeBody       ExtractorType = "body"
)

var ErrNotImplemented = errors.New("extractor not implemented")

// ResponseContext is the main context interface that all extractors receive.
// Concrete implementations can opt-in to specific capability interfaces.
type ResponseContext interface {
	// All extractors can query what capabilities are available
	HasCapability(cap string) bool
}

// StatusReader provides access to HTTP status code
type StatusReader interface {
	GetStatus() int
}

// HeaderAccessor provides access to HTTP headers
type HeaderAccessor interface {
	GetHeader(key string) string
	Headers() http.Header
}

// BodyReader provides access to the response body as a stream
type BodyReader interface {
	GetBody() io.Reader
}

// ParsedBodyReader provides access to pre-parsed body (JSON, XML, etc.)
type ParsedBodyReader interface {
	GetParsedBody() interface{}
	GetRawBody() []byte
}

// TimingInfo provides access to response timing information
type TimingInfo interface {
	GetDuration() interface{} // Can be used for future timing metrics
}
