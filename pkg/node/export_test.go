package node

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// ClassifyRequestErrorForTest exposes the unexported transport-error classifier
// so black-box tests can verify the user-facing code and message.
func ClassifyRequestErrorForTest(rawURL string, err error) *spi.UserError {
	return classifyRequestError(rawURL, err)
}

// RunAssertionsForTest exposes the unexported runAssertions so tests can verify
// that every assertion outcome is recorded.
func RunAssertionsForTest(
	n *RequestNode, ctx extractors.ResponseContext,
) ([]spi.AssertionResult, error) {
	return n.runAssertions(ctx)
}

// ProcessResponseForTest exposes the unexported processResponse so tests can
// exercise the assert -> extract -> validate -> build path without HTTP.
func ProcessResponseForTest(
	n *RequestNode,
	inputs map[string]any,
	url string,
	headers map[string]string,
	body any,
	resp *http.Response,
	respBody []byte,
	startTime time.Time,
) (spi.AnyResult, error) {
	return n.processResponse(inputs, url, headers, body, resp, respBody, startTime)
}

func NodeHTTPClientForTest() *http.Client {
	return nodeHTTPClient()
}

func CloudLocalDialGuardEnvForTest() string {
	return cloudLocalDialGuardEnv
}

func CloudLocalAddressErrorForTest() error {
	return errCloudLocalAddress
}

func CloudDialMaxRedirectsForTest() int {
	return cloudDialMaxRedirects
}

// CloudDialHooksForTest drives the Cloud dial guard with fixed answers.
type CloudDialHooksForTest struct {
	IPs       []netip.Addr
	LookupErr error
	Local     []netip.Addr
	LocalErr  error
	Dial      func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (h CloudDialHooksForTest) guard() dialGuard {
	return dialGuard{
		lookupIP: func(context.Context, string) ([]netip.Addr, error) {
			return h.IPs, h.LookupErr
		},
		localIPs: func() ([]netip.Addr, error) {
			return h.Local, h.LocalErr
		},
		dial: h.Dial,
	}
}

func (h CloudDialHooksForTest) DialContext(
	ctx context.Context, network, addr string,
) (net.Conn, error) {
	return h.guard().DialContext(ctx, network, addr)
}

func (h CloudDialHooksForTest) CheckRedirect(req *http.Request, via []*http.Request) error {
	return h.guard().CheckRedirect(req, via)
}

func StoppedDialForTest(record func(addr string)) func(context.Context, string, string) (net.Conn, error) {
	return func(_ context.Context, _ string, addr string) (net.Conn, error) {
		record(addr)
		return nil, errors.New("dial stopped")
	}
}

func PrepareRequestForTest(
	n *RequestNode,
	inputs map[string]any,
) (string, map[string]string, any, error) {
	return n.prepareRequest(inputs)
}

func WebhookWaitTimeoutMsForTest(n *WebhookWaitNode) int {
	return n.timeoutMs()
}

func CreateResponseBackedErrorResultForTest(
	n *RequestNode,
	inputs map[string]any,
	url string,
	headers map[string]string,
	body any,
	resp *http.Response,
	respBody []byte,
	parsedBody any,
	err error,
	duration time.Duration,
) spi.AnyResult {
	return n.createResponseBackedErrorResult(
		inputs, url, headers, body, resp, respBody, parsedBody, nil, err, duration,
	)
}
