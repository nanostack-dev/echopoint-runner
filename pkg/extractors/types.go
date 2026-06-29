package extractors

import (
	"errors"
	"net/http"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type AnyExtractor interface {
	Extract(ctx ResponseContext) (any, error)
	GetType() spi.ExtractorType
}

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

// ParsedBodyReader provides access to pre-parsed body (JSON, XML, etc.)
type ParsedBodyReader interface {
	GetParsedBody() any
	GetRawBody() []byte
}
