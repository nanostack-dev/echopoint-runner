package node

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// requestTransportAttempts is how many times a request node sends a request
// whose connection failed in a retryable way: the first send and two retries.
const requestTransportAttempts = 3

// requestTransportBackoff is the base gap before a retry; retry i waits i times
// it, so a request that fails three times spends 300 ms waiting.
const requestTransportBackoff = 100 * time.Millisecond

// retryableTransportError reports whether a request that failed with err, before
// any response arrived, may be sent again without changing what the target sees.
//
// A connection that was never established (a failed dial, a refused connection)
// carried no request, so every method retries. A connection reset or closed
// after the request was written may have reached the server, so only an
// idempotent request (RFC 9110 §9.2.2), or one carrying an idempotency key,
// retries: sending a POST twice could create two resources. Timeouts, DNS,
// TLS and cancellation never retry; they are deterministic or already spent
// the node's time budget.
func retryableTransportError(method string, header http.Header, err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return false
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return false
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "dial" {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return idempotentRequest(method, header)
	}
	return false
}

// idempotentRequest mirrors net/http's replayable rule, adding PUT and DELETE,
// which RFC 9110 defines as idempotent.
func idempotentRequest(method string, header http.Header) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete:
		return true
	}
	return header.Get("Idempotency-Key") != "" || header.Get("X-Idempotency-Key") != ""
}

// waitBeforeRetry sleeps before retry number attempt, or returns the context's
// error when the node is cancelled or runs out of time first.
func waitBeforeRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt) * requestTransportBackoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
