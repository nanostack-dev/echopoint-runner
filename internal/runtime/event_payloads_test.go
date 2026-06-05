//nolint:testpackage // white-box: asserts the wire shape of unexported payload builders
package runtime

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

// toMap marshals the typed payload and decodes it back to a generic map so the
// test asserts on the actual wire shape (the contract the control plane reads).
func toMap(t *testing.T, v any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if unmarshalErr := json.Unmarshal(encoded, &m); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v", unmarshalErr)
	}
	return m
}

func TestRequestResultPayload_WireShape(t *testing.T) {
	result := &node.RequestExecutionResult{
		BaseExecutionResult: node.BaseExecutionResult{
			NodeID:      "ping",
			DisplayName: "Ping",
			NodeType:    node.TypeRequest,
			Outputs:     map[string]any{"id": "prd_1"},
		},
		RequestMethod:      "GET",
		RequestURL:         "https://example.test/x",
		RequestHeaders:     map[string]string{"Accept": "application/json"},
		ResponseStatusCode: 200,
		ResponseHeaders:    map[string][]string{"Content-Type": {"application/json"}},
		AssertionResults: []node.AssertionResult{
			{Index: 0, Extractor: "statusCode", Operator: "equals", Expected: "200", Actual: 200, Passed: true},
		},
		DurationMs: 12,
	}

	m := toMap(t, executionResultToEventPayload(result))

	for _, k := range []string{"node_id", "display_name", "node_type", "outputs", "request", "response", "duration_ms"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %v", k, m)
		}
	}
	if _, ok := m["delay_ms"]; ok {
		t.Error("request result must not emit delay_ms")
	}
	resp, _ := m["response"].(map[string]any)
	if _, ok := resp["assertion_results"]; !ok {
		t.Errorf("response missing assertion_results: %v", resp)
	}
	req, _ := m["request"].(map[string]any)
	if req["method"] != "GET" || req["url"] != "https://example.test/x" {
		t.Errorf("request fields wrong: %v", req)
	}
}

func TestRequestResultPayload_OmitsEmptyAssertionResults(t *testing.T) {
	result := &node.RequestExecutionResult{
		BaseExecutionResult: node.BaseExecutionResult{NodeID: "n", NodeType: node.TypeRequest},
		ResponseStatusCode:  204,
	}
	resp, _ := toMap(t, executionResultToEventPayload(result))["response"].(map[string]any)
	if _, ok := resp["assertion_results"]; ok {
		t.Errorf("empty assertion results must be omitted, got %v", resp)
	}
}

func TestDelayResultPayload_WireShape(t *testing.T) {
	result := &node.DelayExecutionResult{
		BaseExecutionResult: node.BaseExecutionResult{NodeID: "wait", NodeType: node.TypeDelay},
		DelayMs:             500,
	}
	m := toMap(t, executionResultToEventPayload(result))
	if _, ok := m["delay_ms"]; !ok {
		t.Errorf("delay result must emit delay_ms: %v", m)
	}
	if _, ok := m["request"]; ok {
		t.Error("delay result must not emit request")
	}
}

func TestNodeFinishedPayload_SuccessVsFailureShape(t *testing.T) {
	succeeded := true
	success := toMap(t, nodeFinishedPayload{
		NodeID: "n", Success: &succeeded, Result: &nodeResultPayload{NodeID: "n"},
	})
	if success["success"] != true {
		t.Error("success payload must carry success=true")
	}
	if _, ok := success["error"]; ok {
		t.Error("success payload must not carry error")
	}

	failure := toMap(t, nodeFinishedPayload{NodeID: "n", Error: "boom"})
	if failure["error"] != "boom" {
		t.Error("failure payload must carry the error")
	}
	if _, ok := failure["success"]; ok {
		t.Error("failure payload must not carry success")
	}
	if _, ok := failure["result"]; ok {
		t.Error("failure payload must not carry result")
	}
}
