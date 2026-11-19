package node_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func TestClassifyRequestError(t *testing.T) {
	const target = "https://anchorapidev.nanostack.dev/v1/auth/login"

	// The real-world example: DNS lookup failure wrapped in *url.Error.
	dnsCause := &url.Error{
		Op:  "Post",
		URL: target,
		Err: &net.DNSError{Err: "no such host", Name: "anchorapidev.nanostack.dev", IsNotFound: true},
	}

	cases := []struct {
		name     string
		err      error
		wantCode string
		wantMsg  string
	}{
		{
			name:     "dns no such host",
			err:      dnsCause,
			wantCode: "DNS_RESOLUTION_FAILED",
			wantMsg:  `Could not resolve host "anchorapidev.nanostack.dev"`,
		},
		{
			name:     "timeout",
			err:      &url.Error{Op: "Post", URL: target, Err: context.DeadlineExceeded},
			wantCode: "REQUEST_TIMEOUT",
			wantMsg:  "Request to anchorapidev.nanostack.dev timed out",
		},
		{
			// The fallback names the host too: the raw error carries the full URL,
			// query string included, and the message reaches log lines that are
			// never redacted.
			name:     "unknown falls back to the host",
			err:      errors.New("boom"),
			wantCode: "REQUEST_FAILED",
			wantMsg:  "Request to anchorapidev.nanostack.dev failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := node.ClassifyRequestErrorForTest(target, tc.err)
			if got.Code != tc.wantCode {
				t.Fatalf("code: got %q want %q", got.Code, tc.wantCode)
			}
			if got.Message != tc.wantMsg {
				t.Fatalf("message: got %q want %q", got.Message, tc.wantMsg)
			}
			// Always a UserError so the engine logs it at debug, and the original
			// cause stays reachable for diagnostics.
			if _, ok := spi.AsUserError(got); !ok {
				t.Fatal("expected a UserError")
			}
			// The cause is the transport error the *url.Error wraps: the wrapper
			// only adds the URL, which the error text must not repeat.
			want := tc.err
			if urlErr, ok := errors.AsType[*url.Error](tc.err); ok {
				want = urlErr.Err
			}
			if !errors.Is(got, want) {
				t.Fatalf("expected the transport cause to remain reachable via errors.Is")
			}
		})
	}
}

func TestClassifyRequestErrorRawIsNotLeakedForKnownKinds(t *testing.T) {
	dns := &url.Error{Op: "Post", URL: "https://x.example", Err: &net.DNSError{Err: "no such host", Name: "x.example"}}
	got := node.ClassifyRequestErrorForTest("https://x.example", dns)
	// The user-facing message must not contain the raw Go transport noise.
	for _, frag := range []string{"dial tcp", "lookup", `Post "`} {
		if strings.Contains(got.Message, frag) {
			t.Fatalf("message should be clean of %q, got %q", frag, got.Message)
		}
	}
}

// The message reaches the failure log line, which is never redacted, so it must
// name the host only — never the query string a secret is usually carried in.
func TestClassifyRequestError_MessageDropsTheQueryString(t *testing.T) {
	const secret = "sk-live-1"
	target := "https://x.example/v1/login?token=" + secret

	for _, cause := range []error{
		// The raw Go transport error repeats the URL it was given.
		fmt.Errorf("Post %q: boom", target),
		&url.Error{Op: "Post", URL: target, Err: errors.New("boom")},
	} {
		got := node.ClassifyRequestErrorForTest(target, cause)
		if strings.Contains(got.Message, secret) {
			t.Fatalf("message leaked the query: %q", got.Message)
		}
	}
}

// A URL the parser cannot split into a host — a relative target, or one holding
// a control character — falls back to the raw string, which must still be cut at
// the query.
func TestClassifyRequestError_UnparseableURLStillDropsTheQueryString(t *testing.T) {
	const secret = "sk-live-1"

	for _, target := range []string{
		"/v1/login?token=" + secret,
		"http://\x7f/v1/login?token=" + secret,
	} {
		got := node.ClassifyRequestErrorForTest(target, errors.New("boom"))
		if strings.Contains(got.Message, secret) {
			t.Fatalf("message leaked the query of %q: %q", target, got.Message)
		}
	}
}

// Error() appends the cause to the message, and that text is the node.failed
// event error and the execution error_message. The *url.Error wrapper repeats
// the URL as Go escaped it, a form of a secret that text masking may miss, so
// the cause keeps only the transport error it wraps.
func TestClassifyRequestError_ErrorTextDropsTheURLAndKeepsTheTransportCause(t *testing.T) {
	const target = "https://x.example/v1/p@ss%20w/rd?token=p%40ss+w%2Frd"
	cause := &url.Error{
		Op:  "Get",
		URL: target,
		Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET},
	}

	got := node.ClassifyRequestErrorForTest(target, cause)

	text := got.Error()
	for _, fragment := range []string{`Get "`, "/v1/", "token=", "p%40ss", "p@ss"} {
		if strings.Contains(text, fragment) {
			t.Errorf("error text carries %q: %q", fragment, text)
		}
	}
	if !strings.Contains(text, "connection reset by peer") {
		t.Errorf("error text lost the transport cause: %q", text)
	}
	if got.Code != "CONNECTION_FAILED" {
		t.Errorf("code: got %q want CONNECTION_FAILED", got.Code)
	}
	if !errors.Is(got, syscall.ECONNRESET) {
		t.Error("the transport cause must stay reachable via errors.Is")
	}
}

// The code is classified on the full error. A *url.Error is itself a net.Error,
// so an unrecognized transport failure inside one keeps CONNECTION_FAILED even
// though the cause drops the wrapper.
func TestClassifyRequestError_KeepsTheCodeOfAnUnknownErrorInsideAURLError(t *testing.T) {
	const target = "https://x.example/v1"
	got := node.ClassifyRequestErrorForTest(target, &url.Error{Op: "Get", URL: target, Err: errors.New("boom")})
	if got.Code != "CONNECTION_FAILED" {
		t.Errorf("code: got %q want CONNECTION_FAILED", got.Code)
	}
	if got.Error() != "Could not connect to x.example: boom" {
		t.Errorf("error text: got %q", got.Error())
	}
}
