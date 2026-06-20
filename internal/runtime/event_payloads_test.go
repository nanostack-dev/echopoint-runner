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

//go:fix inline
func ptrStr(s string) *string { return new(s) }

// The node result is shipped as the engine's own AnyExecutionResult, so the wire
// shape is the flat engine shape (request_method, response_status_code,
// assertion_results, …) that the control plane decodes with
// DecodeAnyExecutionResult — not a parallel nested struct.
func TestNodeFinishedPayload_CarriesFlatEngineResult(t *testing.T) {
	succeeded := true
	result := &node.RequestExecutionResult{
		BaseExecutionResult: node.BaseExecutionResult{
			NodeID:      "ping",
			DisplayName: "Ping",
			NodeType:    node.TypeRequest,
			Outputs:     map[string]any{"id": "prd_1"},
		},
		RequestMethod:      "GET",
		RequestURL:         "https://example.test/x",
		ResponseStatusCode: 200,
		AssertionResults: []node.AssertionResult{
			{Index: 0, Extractor: "statusCode", Operator: "equals", Expected: "200", Actual: 200, Passed: true},
		},
		DurationMs: 12,
	}

	m := toMap(t, nodeFinishedPayload{NodeID: "ping", Success: &succeeded, Result: result})
	resultMap, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("result missing or not an object: %v", m)
	}

	for _, k := range []string{
		"node_id", "node_type", "outputs",
		"request_method", "request_url", "response_status_code", "assertion_results",
	} {
		if _, present := resultMap[k]; !present {
			t.Errorf("flat result missing key %q in %v", k, resultMap)
		}
	}
	// Must NOT be the old nested shape.
	if _, nested := resultMap["request"].(map[string]any); nested {
		t.Error("result must be flat, not nested under request{}")
	}
}

// Skipped nodes must carry the reason and missing inputs over the wire so the
// control plane records them as skipped.
func TestNodeFinishedPayload_CarriesSkipFields(t *testing.T) {
	succeeded := true
	result := &node.RequestExecutionResult{
		BaseExecutionResult: node.BaseExecutionResult{
			NodeID:        "notify",
			NodeType:      node.TypeRequest,
			SkipReason:    new("dependency_failed"),
			ErrorMsg:      new(`Skipped because step "Create" failed`),
			MissingInputs: []string{"create.id"},
		},
	}

	resultMap, ok := toMap(t, nodeFinishedPayload{NodeID: "notify", Success: &succeeded, Result: result})["result"].(map[string]any)
	if !ok {
		t.Fatal("result missing")
	}
	if resultMap["skip_reason"] != "dependency_failed" {
		t.Errorf("skip_reason not carried: %v", resultMap["skip_reason"])
	}
	if resultMap["error_message"] != `Skipped because step "Create" failed` {
		t.Errorf("error_message not carried: %v", resultMap["error_message"])
	}
	if _, present := resultMap["missing_inputs"]; !present {
		t.Errorf("missing_inputs not carried: %v", resultMap)
	}
}

func TestNodeFinishedPayload_SuccessVsFailureShape(t *testing.T) {
	succeeded := true
	success := toMap(t, nodeFinishedPayload{NodeID: "n", Success: &succeeded})
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
}
