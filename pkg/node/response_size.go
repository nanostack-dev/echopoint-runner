package node

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"

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
// first bytes are body. Up to ReportedResponseBodyBytes it reports the parsed
// form alone when that form carries the body (see parsedCarriesBody), and the
// raw bytes otherwise. Past the limit it keeps the first bytes, copied when
// body holds more so the full body can be released, and drops the parsed tree.
func reportedBody(body []byte, size int64, parsed any) ([]byte, any, bool) {
	if size <= ReportedResponseBodyBytes {
		if parsedCarriesBody(body, parsed) {
			return nil, parsed, false
		}
		return body, parsed, false
	}
	if len(body) > ReportedResponseBodyBytes {
		return bytes.Clone(body[:ReportedResponseBodyBytes]), nil, true
	}
	return body, nil, true
}

// parsedCarriesBody reports whether a result's parsed body makes its raw copy
// redundant (ADR 0001). The parsed form is the JSON tree or the body as text,
// so it holds the body unless the bytes are not valid UTF-8, which JSON text
// cannot carry, or contain NUL, which Echopoint stores as U+FFFD. A JSON tree
// also loses a string escape of NUL or of an unpaired surrogate, which decode
// to the same characters. A JSON null parses to nil and has only its raw form.
//
// A JSON tree does not keep whitespace, key order, escapes or the digits of a
// number past float64 precision. Regex checks and string operators on a JSON
// object read the wire bytes instead, so a failed result reports them too
// (RequestExecutionResult.Fail).
func parsedCarriesBody(body []byte, parsed any) bool {
	if parsed == nil || !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return false
	}
	return parsedIsBodyText(body, parsed) || !hasLossyJSONEscape(body)
}

// parsedIsBodyText reports whether parsed is the body verbatim: a body that is
// not JSON, or JSON that failed to parse.
func parsedIsBodyText(body []byte, parsed any) bool {
	text, ok := parsed.(string)
	return ok && text == string(body)
}

// hasLossyJSONEscape reports whether the JSON text body holds a \u escape that
// encoding/json decodes to a character the stored tree cannot keep: NUL, or an
// unpaired surrogate, which becomes U+FFFD. A valid surrogate pair is kept.
func hasLossyJSONEscape(body []byte) bool {
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		r, ok := unicodeEscape(body, i)
		switch {
		case !ok:
			i++ // a two-byte escape such as \\ or \"
		case r == 0:
			return true
		case utf16.IsSurrogate(r) && r < lowSurrogateStart:
			low, paired := unicodeEscape(body, i+unicodeEscapeLen)
			if !paired || !utf16.IsSurrogate(low) || low < lowSurrogateStart {
				return true
			}
			i += surrogatePairEscapeLen - 1
		case utf16.IsSurrogate(r):
			return true
		default:
			i += unicodeEscapeLen - 1
		}
	}
	return false
}

const (
	unicodeEscapeLen       = len(`\u0000`)
	surrogatePairEscapeLen = len(`\ud83d\ude00`)
	lowSurrogateStart      = 0xDC00
)

// unicodeEscape decodes the \uXXXX escape that starts at body[i].
func unicodeEscape(body []byte, i int) (rune, bool) {
	if i+unicodeEscapeLen > len(body) || body[i] != '\\' || body[i+1] != 'u' {
		return 0, false
	}
	var unit [2]byte
	if _, err := hex.Decode(unit[:], body[i+2:i+unicodeEscapeLen]); err != nil {
		return 0, false
	}
	return rune(binary.BigEndian.Uint16(unit[:])), true
}
