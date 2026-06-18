package node

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// classifyRequestError turns a transport-level failure from an outbound request
// node into a UserError with a stable code and a clean, user-facing message.
//
// These failures originate from the user's target endpoint or their network
// configuration (DNS, connection, TLS, timeout), not from the runner, so the
// raw Go error (e.g. `Post "https://...": dial tcp: lookup host: no such host`)
// is replaced with something actionable and the failure is classified as a
// UserError. Anything the classifier doesn't recognize falls back to a generic
// REQUEST_FAILED carrying the original message.
func classifyRequestError(rawURL string, err error) *spi.UserError {
	host := rawURL
	if u, perr := url.Parse(rawURL); perr == nil && u.Host != "" {
		host = u.Host
	}

	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		return spi.NewUserError(
			"DNS_RESOLUTION_FAILED",
			fmt.Sprintf("Could not resolve host %q", dnsErr.Name),
			err,
		)
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return spi.NewUserError(
			"REQUEST_TIMEOUT",
			fmt.Sprintf("Request to %s timed out", host),
			err,
		)
	case errors.Is(err, syscall.ECONNREFUSED):
		return spi.NewUserError(
			"CONNECTION_REFUSED",
			fmt.Sprintf("Connection refused by %s", host),
			err,
		)
	case isTLSError(err):
		return spi.NewUserError(
			"TLS_ERROR",
			fmt.Sprintf("TLS handshake with %s failed", host),
			err,
		)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return spi.NewUserError(
			"CONNECTION_FAILED",
			fmt.Sprintf("Could not connect to %s", host),
			err,
		)
	}

	return spi.NewUserError("REQUEST_FAILED", err.Error(), err)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isTLSError(err error) bool {
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return true
	}
	var recordErr tls.RecordHeaderError
	return errors.As(err, &recordErr)
}
