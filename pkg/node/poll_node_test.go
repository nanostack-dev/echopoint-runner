package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// fakePollExecutor is a stub spi.ModuleExecutor that returns a scripted sequence
// of FinalOutputs (one per attempt) and records the requests it received.
type fakePollExecutor struct {
	// outputs[i] is the FinalOutputs returned on the (i+1)-th call; the last entry
	// is reused once exhausted.
	outputs []map[string]any
	// err, when set, is returned instead of a result (simulates a body failure).
	err error

	calls    int
	requests []spi.ModuleExecutionRequest
}

func (f *fakePollExecutor) ExecuteModule(req spi.ModuleExecutionRequest) (*spi.FlowExecutionResult, error) {
	f.calls++
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	idx := f.calls - 1
	if idx >= len(f.outputs) {
		idx = len(f.outputs) - 1
	}
	return &spi.FlowExecutionResult{
		FinalOutputs: f.outputs[idx],
		Success:      true,
	}, nil
}

var _ spi.ModuleExecutor = (*fakePollExecutor)(nil)

// mkPollNodeJSON builds a poll node wire payload with a single jsonPath exit
// assertion and a 1ms interval (to keep tests fast). Omitting the assertion
// (assert=false) exercises the no-assertion guard.
func mkPollNodeJSON(t *testing.T, maxAttempts int, path, value string, assert bool) []byte {
	t.Helper()
	assertions := "[]"
	if assert {
		assertions = fmt.Sprintf(
			`[{"extractor_type":"jsonPath","extractor_data":{"path":%q},`+
				`"operator_type":"equals","operator_data":{"value":%q}}]`,
			path, value,
		)
	}
	return fmt.Appendf(nil,
		`{"id":"poll-1","type":"poll","assertions":%s,`+
			`"data":{"body":{"nodes":[],"edges":[]},"max_attempts":%d,"interval_ms":1}}`,
		assertions, maxAttempts,
	)
}

func decodePollNode(t *testing.T, raw []byte) node.AnyNode {
	t.Helper()
	n, err := node.UnmarshalNode(raw)
	if err != nil {
		t.Fatalf("UnmarshalNode: %v", err)
	}
	if n.GetType() != spi.KindPoll {
		t.Fatalf("expected poll type, got %s", n.GetType())
	}
	return n
}

func TestPollNode_Decode(t *testing.T) {
	n := decodePollNode(t, mkPollNodeJSON(t, 5, "$.status", "done", true))
	if n.GetRunWhen() != spi.RunWhenOnSuccess {
		t.Errorf("spi.RunWhen should default to on_success, got %s", n.GetRunWhen())
	}
	pn, ok := node.AsPollNode(n)
	if !ok {
		t.Fatalf("expected *PollNode, got %T", n)
	}
	if pn.GetData().MaxAttempts != 5 || pn.GetData().IntervalMs != 1 {
		t.Errorf("unexpected poll data: %+v", pn.GetData())
	}
	if len(n.GetAssertions()) != 1 {
		t.Errorf("expected 1 exit-condition assertion, got %d", len(n.GetAssertions()))
	}
}

func TestPollNode_SucceedsAfterPendingAttempts(t *testing.T) {
	n := decodePollNode(t, mkPollNodeJSON(t, 5, "$.status", "done", true))
	exec := &fakePollExecutor{outputs: []map[string]any{
		{"status": "pending"},
		{"status": "pending"},
		{"status": "done"},
	}}

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{"jobId": "abc"},
		ModuleExecutor: exec,
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	pollRes, ok := spi.As[*node.PollExecutionResult](res)
	if !ok {
		t.Fatalf("expected *PollExecutionResult, got %T", res)
	}
	if pollRes.Attempts != 3 {
		t.Errorf("expected Attempts=3, got %d", pollRes.Attempts)
	}
	if exec.calls != 3 {
		t.Errorf("expected 3 body executions, got %d", exec.calls)
	}
	if got := pollRes.Outputs["attempts"]; got != 3 {
		t.Errorf("expected outputs.attempts=3, got %v", got)
	}
	result, ok := pollRes.Outputs["result"].(map[string]any)
	if !ok || result["status"] != "done" {
		t.Errorf("expected outputs.result.status=done, got %v", pollRes.Outputs["result"])
	}
	if res.GetError() != nil {
		t.Errorf("success result should carry no error, got %v", res.GetError())
	}
	// Each attempt forwards flow inputs plus the attempt counter.
	if len(exec.requests) != 3 {
		t.Fatalf("expected 3 recorded requests, got %d", len(exec.requests))
	}
	if exec.requests[0].Inputs["jobId"] != "abc" {
		t.Errorf("expected flow inputs forwarded to body, got %v", exec.requests[0].Inputs)
	}
	if exec.requests[2].Inputs["attempt"] != 3 {
		t.Errorf("expected attempt counter injected, got %v", exec.requests[2].Inputs["attempt"])
	}
}

func TestPollNode_NeverSatisfiedFails(t *testing.T) {
	n := decodePollNode(t, mkPollNodeJSON(t, 2, "$.status", "done", true))
	exec := &fakePollExecutor{outputs: []map[string]any{{"status": "pending"}}}

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	if err == nil {
		t.Fatal("expected error when condition never met")
	}
	if exec.calls != 2 {
		t.Errorf("expected exactly max_attempts=2 body executions, got %d", exec.calls)
	}
	pollRes, ok := spi.As[*node.PollExecutionResult](res)
	if !ok {
		t.Fatalf("expected *PollExecutionResult, got %T", res)
	}
	if pollRes.Attempts != 2 {
		t.Errorf("expected Attempts=2, got %d", pollRes.Attempts)
	}
	if pollRes.ErrorCode == nil || *pollRes.ErrorCode != "POLL_CONDITION_NOT_MET" {
		t.Errorf("expected POLL_CONDITION_NOT_MET error code, got %v", pollRes.ErrorCode)
	}
	if len(pollRes.AssertionResults) != 1 || pollRes.AssertionResults[0].Passed {
		t.Errorf("expected last (failing) assertion captured, got %+v", pollRes.AssertionResults)
	}
}

func TestPollNode_BodyErrorFails(t *testing.T) {
	n := decodePollNode(t, mkPollNodeJSON(t, 5, "$.status", "done", true))
	exec := &fakePollExecutor{err: errors.New("boom")}

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	if err == nil {
		t.Fatal("expected error when body execution fails")
	}
	if exec.calls != 1 {
		t.Errorf("expected to stop after first body failure, got %d calls", exec.calls)
	}
	pollRes := spi.MustAs[*node.PollExecutionResult](res)
	if pollRes.ErrorCode == nil || *pollRes.ErrorCode != "POLL_BODY_FAILED" {
		t.Errorf("expected POLL_BODY_FAILED error code, got %v", pollRes.ErrorCode)
	}
}

func TestPollNode_ContextCancellationAborts(t *testing.T) {
	n := decodePollNode(t, mkPollNodeJSON(t, 5, "$.status", "done", true))
	exec := &fakePollExecutor{outputs: []map[string]any{{"status": "pending"}}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the first attempt

	res, err := n.Execute(spi.ExecutionContext{
		Ctx:            ctx,
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if exec.calls != 0 {
		t.Errorf("expected no body executions on pre-cancelled context, got %d", exec.calls)
	}
	pollRes := spi.MustAs[*node.PollExecutionResult](res)
	if pollRes.ErrorCode == nil || *pollRes.ErrorCode != "POLL_CANCELLED" {
		t.Errorf("expected POLL_CANCELLED error code, got %v", pollRes.ErrorCode)
	}
}

func TestPollNode_NoAssertionsFails(t *testing.T) {
	n := decodePollNode(t, mkPollNodeJSON(t, 5, "", "", false))
	exec := &fakePollExecutor{outputs: []map[string]any{{"status": "done"}}}

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	if err == nil {
		t.Fatal("expected error when poll has no exit-condition assertions")
	}
	if exec.calls != 0 {
		t.Errorf("expected no body executions without an exit condition, got %d", exec.calls)
	}
	pollRes := spi.MustAs[*node.PollExecutionResult](res)
	if pollRes.ErrorCode == nil || *pollRes.ErrorCode != "POLL_FAILED" {
		t.Errorf("expected POLL_FAILED error code, got %v", pollRes.ErrorCode)
	}
}

func TestPollNode_NoExecutorFails(t *testing.T) {
	n := decodePollNode(t, mkPollNodeJSON(t, 5, "$.status", "done", true))

	_, err := n.Execute(spi.ExecutionContext{
		Inputs:     map[string]any{},
		FlowInputs: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error when spi.ModuleExecutor is nil")
	}
}

// Compile-time sanity: ensure the JSON body we build for tests is valid.
func TestPollNode_BodyIsValidJSON(t *testing.T) {
	pn := node.MustAsPollNode(decodePollNode(t, mkPollNodeJSON(t, 1, "$.status", "done", true)))
	var body map[string]any
	if err := json.Unmarshal(pn.GetData().Body, &body); err != nil {
		t.Fatalf("poll body should be valid JSON: %v", err)
	}
}
