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

// Targeted is the optional interface an extractor implements when it reads one
// named part of the response: a JSON path, a header name, a query parameter. A
// failure message quotes the target so the reader knows what was checked.
// Extractors that read the whole response (status_code, body) do not implement it.
type Targeted interface {
	Target() string
}

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

// QueryParamAccessor provides access to the query parameters of a received request.
type QueryParamAccessor interface {
	GetQueryParam(name string) (string, bool)
}

// TextExtractor is an extractor whose value has a text form that differs from
// its structured value. String operators compare against the text form.
type TextExtractor interface {
	ExtractText(ctx ResponseContext) (any, error)
}
