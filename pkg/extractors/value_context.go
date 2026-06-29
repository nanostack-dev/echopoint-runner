package extractors

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// valueResponseContext is an in-memory ResponseContext over an arbitrary value.
// It is the single adapter the non-HTTP nodes (assert/sse/poll) use to evaluate
// assertions and outputs against a plain Go value instead of an HTTP response.
// The value becomes the parsed body; the raw body is its JSON marshalling.
type valueResponseContext struct {
	value   any
	rawBody []byte
}

// NewValueResponseContext builds a ResponseContext backed by an arbitrary value.
// The value is exposed as the parsed body (GetParsedBody) and its JSON encoding
// as the raw body (GetRawBody / GetBody). Status is 0 and headers are empty —
// only the body / parsed_body capabilities are advertised, which is what the
// jsonPath and body extractors need.
func NewValueResponseContext(value any) ResponseContext {
	raw, err := json.Marshal(value)
	if err != nil {
		// A value that cannot be marshalled still has a usable parsed body; leave
		// the raw body empty rather than failing construction.
		raw = nil
	}
	return &valueResponseContext{
		value:   value,
		rawBody: raw,
	}
}

func (rc *valueResponseContext) HasCapability(capability string) bool {
	switch capability {
	case "body":
		return rc.rawBody != nil
	case "parsed_body":
		return true
	default:
		return false
	}
}

// GetStatus reports 0 — a value context has no HTTP status.
func (rc *valueResponseContext) GetStatus() int { return 0 }

// GetHeader always returns the empty string — a value context has no headers.
func (rc *valueResponseContext) GetHeader(string) string { return "" }

// Headers returns an empty header set.
func (rc *valueResponseContext) Headers() http.Header { return http.Header{} }

// GetBody returns a reader over the JSON-encoded value.
func (rc *valueResponseContext) GetBody() io.Reader {
	return bytes.NewReader(rc.rawBody)
}

// GetParsedBody returns the underlying value as the parsed body.
func (rc *valueResponseContext) GetParsedBody() any { return rc.value }

// GetRawBody returns the JSON-encoded value.
func (rc *valueResponseContext) GetRawBody() []byte { return rc.rawBody }

// Compile-time assertions that the value context satisfies every capability
// interface assert/sse/poll rely on.
var (
	_ ResponseContext  = (*valueResponseContext)(nil)
	_ StatusReader     = (*valueResponseContext)(nil)
	_ HeaderAccessor   = (*valueResponseContext)(nil)
	_ ParsedBodyReader = (*valueResponseContext)(nil)
	_ BodyReader       = (*valueResponseContext)(nil)
)
