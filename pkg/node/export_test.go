package node

import (
	"net/http"
	"time"
)

func PrepareRequestForTest(
	n *RequestNode,
	inputs map[string]interface{},
) (string, map[string]string, interface{}, error) {
	return n.prepareRequest(inputs)
}

func CreateResponseBackedErrorResultForTest(
	n *RequestNode,
	inputs map[string]interface{},
	url string,
	headers map[string]string,
	body interface{},
	resp *http.Response,
	respBody []byte,
	parsedBody interface{},
	err error,
	duration time.Duration,
) AnyExecutionResult {
	return n.createResponseBackedErrorResult(inputs, url, headers, body, resp, respBody, parsedBody, err, duration)
}
