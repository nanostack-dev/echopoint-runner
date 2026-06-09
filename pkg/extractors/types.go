package extractors

import (
	"errors"
	"io"
	"net/http"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type AnyExtractor interface {
	Extract(ctx ResponseContext) (any, error)
	GetType() ExtractorType
}

// ExtractorType is re-exported from spi (the L0 contract). Alias kept for back-compat.
type ExtractorType = spi.ExtractorType

// Built-in extractor types (re-exported from spi).
const (
	ExtractorTypeJSONPath   = spi.ExtractorTypeJSONPath
	ExtractorTypeXMLPath    = spi.ExtractorTypeXMLPath
	ExtractorTypeStatusCode = spi.ExtractorTypeStatusCode
	ExtractorTypeHeader     = spi.ExtractorTypeHeader
	ExtractorTypeBody       = spi.ExtractorTypeBody
)

var ErrNotImplemented = errors.New("extractor not implemented")

// ResponseContext is the main context interface that all extractors receive.
// Concrete implementations can opt-in to specific capability interfaces.
type ResponseContext interface {
	// All extractors can query what capabilities are available
	HasCapability(capability string) bool
}

// StatusReader provides access to HTTP status code.
type StatusReader interface {
	GetStatus() int
}

// HeaderAccessor provides access to HTTP headers.
type HeaderAccessor interface {
	GetHeader(key string) string
	Headers() http.Header
}

// BodyReader provides access to the response body as a stream.
type BodyReader interface {
	GetBody() io.Reader
}

// ParsedBodyReader provides access to pre-parsed body (JSON, XML, etc.)
type ParsedBodyReader interface {
	GetParsedBody() any
	GetRawBody() []byte
}

// TimingInfo provides access to response timing information.
type TimingInfo interface {
	GetDuration() any // Can be used for future timing metrics
}
