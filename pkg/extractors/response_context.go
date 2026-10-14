package extractors

import (
	"io"
	"net/http"
	"sync"
)

const capabilityParsedBody = "parsed_body"

// concreteResponseContext implements all ResponseContext interfaces.
type concreteResponseContext struct {
	resp        *http.Response
	rawBody     []byte
	parsedBody  any
	bodyReader  io.Reader
	contentType string
}

// NewResponseContext creates a new ResponseContext from an HTTP response.
func NewResponseContext(
	resp *http.Response, rawBody []byte, parsedBody any,
) ResponseContext {
	return &concreteResponseContext{
		resp:        resp,
		rawBody:     rawBody,
		parsedBody:  parsedBody,
		bodyReader:  io.NopCloser(io.Reader(nil)),
		contentType: resp.Header.Get("Content-Type"),
	}
}

// ============================================================================
// ResponseContext Interface Implementation
// ============================================================================

func (rc *concreteResponseContext) HasCapability(capability string) bool {
	switch capability {
	case "status":
		return rc.resp != nil
	case "headers":
		return rc.resp != nil
	case "body":
		return rc.rawBody != nil
	case capabilityParsedBody:
		return rc.parsedBody != nil
	case "timing":
		return false // Not implemented yet
	default:
		return false
	}
}
func (rc *concreteResponseContext) GetStatus() int {
	if rc.resp == nil {
		return 0
	}
	return rc.resp.StatusCode
}

func (rc *concreteResponseContext) GetHeader(key string) string {
	if rc.resp == nil {
		return ""
	}
	return rc.resp.Header.Get(key)
}

func (rc *concreteResponseContext) Headers() http.Header {
	if rc.resp == nil {
		return http.Header{}
	}
	return rc.resp.Header
}

func (rc *concreteResponseContext) GetParsedBody() any {
	return rc.parsedBody
}

func (rc *concreteResponseContext) GetRawBody() []byte {
	return rc.rawBody
}

// lazyResponseContext is a concreteResponseContext whose parsed body is built
// on first use, so a large body nothing extracts from is never parsed.
type lazyResponseContext struct {
	concreteResponseContext

	parseOnce sync.Once
	parse     func() any
}

// NewLazyResponseContext is NewResponseContext with the parsed body computed by
// parse when an extractor first asks for it.
func NewLazyResponseContext(resp *http.Response, rawBody []byte, parse func() any) ResponseContext {
	return &lazyResponseContext{
		concreteResponseContext: concreteResponseContext{
			resp:        resp,
			rawBody:     rawBody,
			bodyReader:  io.NopCloser(io.Reader(nil)),
			contentType: resp.Header.Get("Content-Type"),
		},
		parse: parse,
	}
}

func (rc *lazyResponseContext) GetParsedBody() any {
	rc.parseOnce.Do(func() {
		rc.parsedBody = rc.parse()
		rc.parse = nil
	})
	return rc.parsedBody
}

func (rc *lazyResponseContext) HasCapability(capability string) bool {
	if capability == capabilityParsedBody {
		return rc.GetParsedBody() != nil
	}
	return rc.concreteResponseContext.HasCapability(capability)
}
