package node

import (
	"net/http"
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
	return n.processResponse(inputs, requestExchange{
		URL: url, Headers: headers, Body: body, Response: resp, ResponseBody: respBody,
	}, startTime)
}

func PrepareRequestForTest(
	n *RequestNode,
	inputs map[string]any,
) (string, map[string]string, any, error) {
	exchange, err := n.prepareRequest(inputs)
	return exchange.URL, exchange.Headers, exchange.Body, err
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
		inputs, requestExchange{
			URL: url, Headers: headers, Body: body, Response: resp, ResponseBody: respBody, ParsedBody: parsedBody,
		}, nil, err, duration,
	)
}
