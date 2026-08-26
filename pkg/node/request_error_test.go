package node_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
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
			if !errors.Is(got, tc.err) {
				t.Fatalf("expected wrapped cause to remain reachable via errors.Is")
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
