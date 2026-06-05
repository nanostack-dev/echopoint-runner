package node_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

func jsonResponse(status int, body string) (*http.Response, []byte) {
	resp := &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	return resp, []byte(body)
}

func TestProcessResponse_SuccessRecordsAssertions(t *testing.T) {
	n := reqNode(mkAssertion(t, "statusCode", "", "equals", "200"))
	resp, respBody := jsonResponse(200, `{"id":"prd_1"}`)

	result, err := node.ProcessResponseForTest(
		n, map[string]any{}, "https://x.test", nil, nil, resp, respBody, time.Now(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	req, ok := node.As[*node.RequestExecutionResult](result)
	if !ok {
		t.Fatalf("expected a request result, got %T", result)
	}
	if req.ResponseStatusCode != 200 {
		t.Errorf("expected status 200, got %d", req.ResponseStatusCode)
	}
	if len(req.AssertionResults) != 1 || !req.AssertionResults[0].Passed {
		t.Errorf("expected one passing assertion recorded, got %+v", req.AssertionResults)
	}
}

func TestProcessResponse_AssertionFailureIsResponseBacked(t *testing.T) {
	n := reqNode(mkAssertion(t, "statusCode", "", "equals", "500"))
	resp, respBody := jsonResponse(200, `{}`)

	result, err := node.ProcessResponseForTest(
		n, map[string]any{}, "https://x.test", nil, nil, resp, respBody, time.Now(),
	)
	if err == nil {
		t.Fatal("expected the failing assertion to fail the node")
	}
	req, ok := node.As[*node.RequestExecutionResult](result)
	if !ok {
		t.Fatalf("expected a response-backed request result, got %T", result)
	}
	// The result still carries the response and the recorded failing assertion.
	if req.ResponseStatusCode != 200 {
		t.Errorf("expected the response status preserved, got %d", req.ResponseStatusCode)
	}
	if len(req.AssertionResults) != 1 || req.AssertionResults[0].Passed {
		t.Errorf("expected one failing assertion recorded, got %+v", req.AssertionResults)
	}
}
