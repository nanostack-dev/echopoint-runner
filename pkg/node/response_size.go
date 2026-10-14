package node

import (
	"bytes"
	"context"
	"fmt"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const (
	mebibyte = 1 << 20

	// DefaultMaxResponseBytes is the largest response body a request node reads
	// when the execution context sets no limit.
	DefaultMaxResponseBytes int64 = 10 * mebibyte
	// ReportedResponseBodyBytes bounds the response body a request result reports.
	// Assertions and outputs still evaluate against the whole body.
	ReportedResponseBodyBytes = mebibyte

	ResponseTooLargeCode = "RESPONSE_TOO_LARGE"
)

type maxResponseBytesKey struct{}

// WithMaxResponseBytes returns ctx carrying the largest response body a request
// node may read. Zero or less keeps DefaultMaxResponseBytes.
func WithMaxResponseBytes(ctx context.Context, limit int64) context.Context {
	return context.WithValue(ctx, maxResponseBytesKey{}, limit)
}

func maxResponseBytes(ctx context.Context) int64 {
	if limit, ok := ctx.Value(maxResponseBytesKey{}).(int64); ok && limit > 0 {
		return limit
	}
	return DefaultMaxResponseBytes
}

func responseTooLargeError(rawURL string, limit int64) *spi.UserError {
	return spi.NewUserError(
		ResponseTooLargeCode,
		fmt.Sprintf("Response from %s is larger than the %s limit", requestHost(rawURL), formatBytes(limit)),
		nil,
	)
}

func formatBytes(n int64) string {
	if n%mebibyte == 0 {
		return fmt.Sprintf("%d MiB", n/mebibyte)
	}
	return fmt.Sprintf("%d bytes", n)
}

// reportedBody is the body a result carries for a body of size bytes whose
// first bytes are body: past ReportedResponseBodyBytes it keeps the first
// bytes, copied when body holds more so the full body can be released, and
// drops the parsed tree.
func reportedBody(body []byte, size int64, parsed any) ([]byte, any, bool) {
	if size <= ReportedResponseBodyBytes {
		return body, parsed, false
	}
	if len(body) > ReportedResponseBodyBytes {
		return bytes.Clone(body[:ReportedResponseBodyBytes]), nil, true
	}
	return body, nil, true
}
