package spi

import (
	"context"
	"net/http"
	"regexp"
)

const (
	TraceparentHeader = "Traceparent"
	TracestateHeader  = "Tracestate"
	maxTracestateLen  = 512
)

var traceparentPattern = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// TraceContext is the W3C trace context the control plane sends with a claimed
// job. The runner copies it onto its own control-plane calls for that job so the
// server can join them to the claim's trace. It never reaches flow target URLs.
type TraceContext struct {
	Traceparent string `json:"traceparent,omitempty"`
	Tracestate  string `json:"tracestate,omitempty"`
}

// Sanitized drops a traceparent that is not W3C-shaped, and a tracestate without
// a valid traceparent or with bytes that cannot be sent in a header.
func (t TraceContext) Sanitized() TraceContext {
	if !traceparentPattern.MatchString(t.Traceparent) {
		return TraceContext{}
	}
	if !validTracestate(t.Tracestate) {
		t.Tracestate = ""
	}
	return t
}

func validTracestate(value string) bool {
	if len(value) > maxTracestateLen {
		return false
	}
	for i := range len(value) {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

// SetHeaders writes the sanitized trace headers onto header. An empty or
// invalid trace context writes nothing.
func (t TraceContext) SetHeaders(header http.Header) {
	clean := t.Sanitized()
	if clean.Traceparent == "" {
		return
	}
	header.Set(TraceparentHeader, clean.Traceparent)
	if clean.Tracestate != "" {
		header.Set(TracestateHeader, clean.Tracestate)
	}
}

type traceContextKey struct{}

// WithTraceContext attaches the claimed job's trace context to ctx. Only
// control-plane calls for that job read it; node requests to target URLs do not.
func WithTraceContext(ctx context.Context, trace TraceContext) context.Context {
	clean := trace.Sanitized()
	if clean.Traceparent == "" {
		return ctx
	}
	return context.WithValue(ctx, traceContextKey{}, clean)
}

// TraceContextFromContext returns the trace context WithTraceContext attached.
func TraceContextFromContext(ctx context.Context) TraceContext {
	if ctx == nil {
		return TraceContext{}
	}
	trace, _ := ctx.Value(traceContextKey{}).(TraceContext)
	return trace
}
