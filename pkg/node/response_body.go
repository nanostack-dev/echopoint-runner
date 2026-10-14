package node

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// unknownLengthFirstRead is what a body without Content-Length reserves before
// its first read. Most such bodies are small and end within it; a longer one
// then reserves up to the bytes it may keep.
const unknownLengthFirstRead int64 = 64 << 10

const minBodyGrowth = 512

// responseBody is a read response body: the first bytes of it a node keeps,
// the whole size, and the budget reservation holding the kept bytes.
type responseBody struct {
	data []byte
	size int64
	hold *bodyReservation
}

// readResponseBody reads resp's body, failing past limit bytes. It keeps at most
// keep bytes in memory, reserved from the context's response budget, and only
// counts the rest. On error the reservation is already released.
func readResponseBody(ctx context.Context, resp *http.Response, url string, limit, keep int64) (responseBody, error) {
	if resp.ContentLength > limit {
		return responseBody{}, responseTooLargeError(url, limit)
	}
	keep = min(keep, limit)
	firstRead := unknownLengthFirstRead
	if resp.ContentLength >= 0 {
		firstRead = resp.ContentLength
	}
	reserve := min(keep, firstRead)
	hold, err := reserveResponseBytes(ctx, reserve)
	if err != nil {
		return responseBody{}, budgetWaitError(url, err)
	}

	body, err := readKept(ctx, resp, url, hold, reserve, keep)
	if err == nil {
		body.size, err = countRest(resp.Body, body.size, limit)
	}
	if err == nil && body.size > limit {
		err = responseTooLargeError(url, limit)
	}
	if err != nil {
		hold.release()
		return responseBody{}, err
	}
	hold.shrinkTo(int64(cap(body.data)))
	body.hold = hold
	return body, nil
}

// readKept reads up to keep bytes into a buffer sized to the reservation. A
// body without Content-Length that outgrows its first read reserves up to keep
// bytes and grows its buffer as it goes.
func readKept(
	ctx context.Context, resp *http.Response, url string, hold *bodyReservation, reserve, keep int64,
) (responseBody, error) {
	data := make([]byte, 0, reserve)
	for {
		read := int64(len(data))
		if read == keep || read == resp.ContentLength {
			return responseBody{data: data, size: read}, nil
		}
		if len(data) == cap(data) {
			if err := hold.growTo(ctx, keep); err != nil {
				return responseBody{}, budgetWaitError(url, err)
			}
			data = slices.Grow(data, int(min(max(read, minBodyGrowth), keep-read)))
		}
		n, err := resp.Body.Read(data[len(data):min(int64(cap(data)), keep)])
		data = data[:len(data)+n]
		if errors.Is(err, io.EOF) {
			return responseBody{data: data, size: int64(len(data))}, nil
		}
		if err != nil {
			return responseBody{}, err
		}
	}
}

// countRest reads the body past the kept bytes without keeping it, stopping one
// byte past limit.
func countRest(body io.Reader, read, limit int64) (int64, error) {
	if read > limit {
		return read, nil
	}
	rest, err := io.Copy(io.Discard, io.LimitReader(body, limit+1-read))
	return read + rest, err
}

// keptResponseBytes is how much of a response body the node keeps in memory:
// the whole body when an assertion or output reads it, otherwise only what its
// result reports.
func (n *RequestNode) keptResponseBytes(limit int64) int64 {
	if n.readsResponseBody() {
		return limit
	}
	return ReportedResponseBodyBytes
}

// readsResponseBody reports whether an assertion or output of the node reads
// the response body. Only status and header extractors are known not to.
func (n *RequestNode) readsResponseBody() bool {
	for _, assertion := range n.Assertions {
		if readsBody(assertion.Extractor) {
			return true
		}
	}
	for _, output := range n.Outputs {
		if readsBody(output.Extractor) {
			return true
		}
	}
	return false
}

func readsBody(extractor interface{ GetType() spi.ExtractorType }) bool {
	if extractor == nil {
		return true
	}
	switch extractor.GetType() {
	case spi.ExtractorTypeStatusCode, spi.ExtractorTypeHeader:
		return false
	case spi.ExtractorTypeJSONPath, spi.ExtractorTypeXMLPath, spi.ExtractorTypeBody, spi.ExtractorTypeQueryParam:
		return true
	default:
		return true
	}
}
