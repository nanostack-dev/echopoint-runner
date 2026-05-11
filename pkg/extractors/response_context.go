package extractors

import (
	"io"
	"net/http"
)

// concreteResponseContext implements all ResponseContext interfaces.
type concreteResponseContext struct {
	resp        *http.Response
	rawBody     []byte
	parsedBody  interface{}
	bodyReader  io.Reader
	contentType string
}

// NewResponseContext creates a new ResponseContext from an HTTP response.
func NewResponseContext(
	resp *http.Response, rawBody []byte, parsedBody interface{},
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
	case "parsed_body":
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

func (rc *concreteResponseContext) GetBody() io.Reader {
	// Return a reader over the raw body (already consumed from response)
	return io.NopCloser(io.NopCloser(io.Reader(nil)))
}
func (rc *concreteResponseContext) GetParsedBody() interface{} {
	return rc.parsedBody
}

func (rc *concreteResponseContext) GetRawBody() []byte {
	return rc.rawBody
}
func (rc *concreteResponseContext) GetDuration() interface{} {
	// Placeholder for future timing information
	return nil
}
