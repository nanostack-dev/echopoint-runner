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

// applyEnginePass mirrors what engine.applyAssertionsAndOutputs does: evaluate
// the node's assertions and outputs against the context the result exposes via
// AssertionContextProvider, recording assertion results and failing the result on
// error. processResponse itself no longer runs assertions — the engine drives
// them — so these node-level tests exercise that exact pass against a request
// result without standing up a full flow.
func applyEnginePass(n *node.RequestNode, res node.AnyExecutionResult) error {
	provider, ok := res.(node.AssertionContextProvider)
	if !ok {
		return nil
	}
	rc := provider.AssertionContext()
	failer := res.(interface {
		SetAssertionResults([]node.AssertionResult)
		Fail(error, string)
		MergeOutputs(map[string]any)
	})

	results, assertErr := node.EvaluateAssertions(n.GetAssertions(), rc)
	failer.SetAssertionResults(results)
	if assertErr != nil {
		failer.Fail(assertErr, "ASSERTION_FAILED")
		return assertErr
	}
	produced, err := node.ExtractOutputs(n.GetOutputs(), rc)
	if err != nil {
		failer.Fail(err, "OUTPUT_EXTRACTION_FAILED")
		return err
	}
	failer.MergeOutputs(produced)
	if validateErr := node.ValidateOutputs(n.OutputSchema(), produced); validateErr != nil {
		failer.Fail(validateErr, "OUTPUT_VALIDATION_FAILED")
		return validateErr
	}
	return nil
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
	// processResponse builds the success result and the assertion context; the
	// engine pass then records the assertion outcomes.
	if passErr := applyEnginePass(n, result); passErr != nil {
		t.Fatalf("unexpected engine-pass err: %v", passErr)
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
	if err != nil {
		t.Fatalf("processResponse should not fail before the engine pass: %v", err)
	}
	if passErr := applyEnginePass(n, result); passErr == nil {
		t.Fatal("expected the failing assertion to fail the node via the engine pass")
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
	if req.GetError() == nil {
		t.Error("expected the result to carry the assertion failure error")
	}
}
