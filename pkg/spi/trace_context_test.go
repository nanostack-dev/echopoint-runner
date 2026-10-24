package spi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
)

const validTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func TestTraceContextSanitizedKeepsAValidTraceparentAndTracestate(t *testing.T) {
	trace := spi.TraceContext{Traceparent: validTraceparent, Tracestate: "echopoint=1,congo=t61rcWkgMzE"}
	assert.Equal(t, trace, trace.Sanitized())
}

func TestTraceContextSanitizedDropsAnInvalidTraceparentWithItsTracestate(t *testing.T) {
	for _, traceparent := range []string{
		"", "garbage", "00-4BF92F3577B34DA6A3CE929D0E0E4736-00F067AA0BA902B7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01\r\nX-Evil: 1",
	} {
		trace := spi.TraceContext{Traceparent: traceparent, Tracestate: "a=b"}
		assert.Equal(t, spi.TraceContext{}, trace.Sanitized(), traceparent)
	}
}

func TestTraceContextSanitizedDropsAnUnsendableTracestate(t *testing.T) {
	for _, tracestate := range []string{"a=b\r\nX-Evil: 1", strings.Repeat("a", 513)} {
		trace := spi.TraceContext{Traceparent: validTraceparent, Tracestate: tracestate}
		assert.Equal(t, spi.TraceContext{Traceparent: validTraceparent}, trace.Sanitized())
	}
}

func TestTraceContextSetHeadersOmitsAnEmptyTracestate(t *testing.T) {
	header := http.Header{}
	spi.TraceContext{Traceparent: validTraceparent}.SetHeaders(header)
	assert.Equal(t, validTraceparent, header.Get("Traceparent"))
	_, hasTracestate := header["Tracestate"]
	assert.False(t, hasTracestate)
}

func TestTraceContextFromContextIsEmptyWithoutOne(t *testing.T) {
	assert.Equal(t, spi.TraceContext{}, spi.TraceContextFromContext(context.Background()))
	invalid := spi.WithTraceContext(context.Background(), spi.TraceContext{Traceparent: "bad"})
	assert.Equal(t, spi.TraceContext{}, spi.TraceContextFromContext(invalid))
}
