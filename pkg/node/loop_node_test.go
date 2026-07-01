package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedCall captures a single ExecuteModule invocation.
type recordedCall struct {
	flowID string
	body   []byte
	inputs map[string]any
}

// fakeModuleExecutor is a test double implementing spi.ModuleExecutor. It records
// every ExecuteModule call and returns a canned result (or error) per call index.
type fakeModuleExecutor struct {
	calls []recordedCall
	// result is returned for every call when err is nil.
	result *spi.FlowExecutionResult
	// errOnCall, when set, returns an error for the given (zero-based) call index.
	errOnCall map[int]error
}

func (f *fakeModuleExecutor) ExecuteModule(
	req spi.ModuleExecutionRequest,
) (*spi.FlowExecutionResult, error) {
	idx := len(f.calls)
	// Snapshot inputs so later mutations by the caller can't corrupt assertions.
	snapshot := make(map[string]any, len(req.Inputs))
	maps.Copy(snapshot, req.Inputs)
	f.calls = append(f.calls, recordedCall{flowID: req.FlowID, body: req.FlowDefinition, inputs: snapshot})

	if f.errOnCall != nil {
		if err, ok := f.errOnCall[idx]; ok {
			return nil, err
		}
	}
	return f.result, nil
}

func cannedResult(outputs map[string]any) *spi.FlowExecutionResult {
	return &spi.FlowExecutionResult{FinalOutputs: outputs, Success: true}
}

func newLoopNode(t *testing.T, data node.LoopData) *node.LoopNode {
	t.Helper()
	return &node.LoopNode{
		BaseNode: node.BaseNode{ID: "loop1", DisplayName: "Loop", NodeType: spi.KindLoop},
		Data:     data,
	}
}

const iterBody = `{"nodes":[{"id":"get","type":"request","data":{"method":"GET","url":"https://api.example.com/{{item}}"}}],"edges":[]}`

func TestLoopNode_IteratesEachItemInjectingItemAndIndex(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{"get.statusCode": float64(200)})}
	n := newLoopNode(t, node.LoopData{
		Items: []any{"a", "b", "c"},
		Body:  json.RawMessage(iterBody),
	})

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{"token": "secret"},
		ModuleExecutor: exec,
	})
	require.NoError(t, err)
	require.Len(t, exec.calls, 3)

	for i, want := range []string{"a", "b", "c"} {
		assert.Equal(t, "loop1#iter", exec.calls[i].flowID)
		assert.Equal(t, want, exec.calls[i].inputs["item"], "item for iteration %d", i)
		assert.Equal(t, i, exec.calls[i].inputs["index"], "index for iteration %d", i)
		// FlowInputs are inherited into each iteration.
		assert.Equal(t, "secret", exec.calls[i].inputs["token"], "token for iteration %d", i)
	}

	loopRes, ok := spi.As[*node.LoopExecutionResult](res)
	require.True(t, ok)
	assert.Equal(t, 3, loopRes.Iterations)

	results, ok := loopRes.Outputs["results"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, results, 3)
	assert.Equal(t, 3, loopRes.Outputs["count"])
}

// applyLoopEnginePass mirrors engine.applyAssertionsAndOutputs: it evaluates the
// loop node's assertions and outputs against the context the result exposes via
// AssertionContextProvider (the {results, count} aggregate). The loop does not
// assert in Execute — the engine drives it — so this exercises that exact pass.
func applyLoopEnginePass(n *node.LoopNode, res spi.AnyResult) error {
	provider, ok := res.(node.AssertionContextProvider)
	if !ok {
		return nil
	}
	rc := provider.AssertionContext()
	if rc == nil {
		return nil
	}
	failer := res.(interface {
		SetAssertionResults([]spi.AssertionResult)
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

func TestLoopNode_ExposesAggregateAssertionContext(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{"get.statusCode": float64(200)})}
	n := newLoopNode(t, node.LoopData{Items: []any{"a", "b", "c"}, Body: json.RawMessage(iterBody)})

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.NoError(t, err)

	// The result opts into the engine-level assertion/output pass.
	provider, ok := res.(node.AssertionContextProvider)
	require.True(t, ok, "LoopExecutionResult must implement AssertionContextProvider")
	rc := provider.AssertionContext()
	require.NotNil(t, rc, "successful loop must expose a non-nil assertion context")

	// The context is the aggregate {results, count}, so assertions/extractors see it.
	pbr, ok := rc.(extractors.ParsedBodyReader)
	require.True(t, ok)
	parsed, ok := pbr.GetParsedBody().(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, parsed["count"])
}

func TestLoopNode_EngineAssertsOnAggregateCount(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{})}
	n := newLoopNode(t, node.LoopData{Items: []any{"a", "b", "c"}, Body: json.RawMessage(iterBody)})
	// Assert on the aggregate count produced by the loop via the engine pass.
	n.Assertions = []node.CompositeAssertion{mkAssertion(t, "jsonPath", "$.count", "equals", "3")}

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.NoError(t, err)
	// Execute itself must NOT run assertions (no double evaluation).
	loopRes := spi.MustAs[*node.LoopExecutionResult](res)
	assert.Empty(t, loopRes.AssertionResults, "Execute must not assert; the engine pass does")

	// The engine pass evaluates the assertion against the aggregate and passes.
	require.NoError(t, applyLoopEnginePass(n, res))
	require.Len(t, loopRes.AssertionResults, 1)
	assert.True(t, loopRes.AssertionResults[0].Passed)
	// The intrinsic outputs survive the pass.
	assert.Equal(t, 3, loopRes.Outputs["count"])
}

func TestLoopNode_EngineFailsOnAggregateAssertionMiss(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{})}
	n := newLoopNode(t, node.LoopData{Items: []any{"a"}, Body: json.RawMessage(iterBody)})
	n.Assertions = []node.CompositeAssertion{mkAssertion(t, "jsonPath", "$.count", "equals", "99")}

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.NoError(t, err)

	passErr := applyLoopEnginePass(n, res)
	require.Error(t, passErr)
	loopRes := spi.MustAs[*node.LoopExecutionResult](res)
	require.NotNil(t, loopRes.ErrorCode)
	assert.Equal(t, "ASSERTION_FAILED", *loopRes.ErrorCode)
}

func TestLoopNode_ResolvesItemsFromTemplateRef(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{})}
	n := newLoopNode(t, node.LoopData{
		Items: "{{{prev.list}}}",
		Body:  json.RawMessage(iterBody),
	})

	_, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{"prev.list": []any{"x", "y"}},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.NoError(t, err)
	require.Len(t, exec.calls, 2)
	assert.Equal(t, "x", exec.calls[0].inputs["item"])
	assert.Equal(t, "y", exec.calls[1].inputs["item"])
}

func TestLoopNode_CustomItemAndIndexVars(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{})}
	n := newLoopNode(t, node.LoopData{
		Items:    []any{"only"},
		Body:     json.RawMessage(iterBody),
		ItemVar:  "id",
		IndexVar: "i",
	})

	_, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.NoError(t, err)
	require.Len(t, exec.calls, 1)
	assert.Equal(t, "only", exec.calls[0].inputs["id"])
	assert.Equal(t, 0, exec.calls[0].inputs["i"])
}

func TestLoopNode_ChildErrorWithoutContinueFailsNode(t *testing.T) {
	exec := &fakeModuleExecutor{
		result:    cannedResult(map[string]any{}),
		errOnCall: map[int]error{1: errors.New("boom")},
	}
	n := newLoopNode(t, node.LoopData{
		Items: []any{"a", "b", "c"},
		Body:  json.RawMessage(iterBody),
	})

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loop iteration 1 failed")
	// Stops after the failing iteration: 2 calls (index 0 ok, index 1 fails).
	assert.Len(t, exec.calls, 2)

	loopRes, ok := spi.As[*node.LoopExecutionResult](res)
	require.True(t, ok)
	require.NotNil(t, loopRes.ErrorCode)
	assert.Equal(t, "LOOP_FAILED", *loopRes.ErrorCode)
	assert.Nil(t, loopRes.Outputs)
}

func TestLoopNode_ContinueOnErrorCapturesAndContinues(t *testing.T) {
	exec := &fakeModuleExecutor{
		result:    cannedResult(map[string]any{"ok": true}),
		errOnCall: map[int]error{1: errors.New("boom")},
	}
	n := newLoopNode(t, node.LoopData{
		Items:           []any{"a", "b", "c"},
		Body:            json.RawMessage(iterBody),
		ContinueOnError: true,
	})

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.NoError(t, err)
	assert.Len(t, exec.calls, 3)

	loopRes := spi.MustAs[*node.LoopExecutionResult](res)
	results, ok := loopRes.Outputs["results"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, results, 3)
	// Index 1 captured the error.
	assert.Equal(t, 1, results[1]["index"])
	assert.Equal(t, "boom", results[1]["error"])
	// Successful iterations carry the canned outputs.
	assert.Equal(t, true, results[0]["ok"])
	assert.Equal(t, true, results[2]["ok"])
}

func TestLoopNode_MaxIterationsCaps(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{})}
	n := newLoopNode(t, node.LoopData{
		Items:         []any{"a", "b", "c", "d", "e"},
		Body:          json.RawMessage(iterBody),
		MaxIterations: 2,
	})

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.NoError(t, err)
	assert.Len(t, exec.calls, 2)

	loopRes := spi.MustAs[*node.LoopExecutionResult](res)
	assert.Equal(t, 2, loopRes.Iterations)
	assert.Equal(t, 2, loopRes.Outputs["count"])
}

func TestLoopNode_EmptyBodyErrors(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{})}
	n := newLoopNode(t, node.LoopData{Items: []any{"a"}})

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loop body is required")
	assert.Empty(t, exec.calls)

	loopRes := spi.MustAs[*node.LoopExecutionResult](res)
	require.NotNil(t, loopRes.ErrorCode)
	assert.Equal(t, "LOOP_FAILED", *loopRes.ErrorCode)
}

func TestLoopNode_NilModuleExecutorErrors(t *testing.T) {
	n := newLoopNode(t, node.LoopData{
		Items: []any{"a"},
		Body:  json.RawMessage(iterBody),
	})

	res, err := n.Execute(spi.ExecutionContext{
		Inputs:     map[string]any{},
		FlowInputs: map[string]any{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module executor unavailable")

	loopRes := spi.MustAs[*node.LoopExecutionResult](res)
	require.NotNil(t, loopRes.ErrorCode)
	assert.Equal(t, "LOOP_FAILED", *loopRes.ErrorCode)
}

func TestLoopNode_NonListItemsErrors(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{})}
	n := newLoopNode(t, node.LoopData{
		Items: "not-a-list",
		Body:  json.RawMessage(iterBody),
	})

	_, err := n.Execute(spi.ExecutionContext{
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must resolve to a list")
	assert.Empty(t, exec.calls)
}

func TestLoopNode_CancelledContextAborts(t *testing.T) {
	exec := &fakeModuleExecutor{result: cannedResult(map[string]any{})}
	n := newLoopNode(t, node.LoopData{
		Items: []any{"a", "b"},
		Body:  json.RawMessage(iterBody),
	})

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := n.Execute(spi.ExecutionContext{
		Ctx:            cancelledCtx,
		Inputs:         map[string]any{},
		FlowInputs:     map[string]any{},
		ModuleExecutor: exec,
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, exec.calls)
}

func TestLoopNode_DecodeViaUnmarshalNode(t *testing.T) {
	raw := `{
		"id": "loop1",
		"type": "loop",
		"data": {
			"items": "{{{prev.ids}}}",
			"item_var": "id",
			"max_iterations": 10,
			"continue_on_error": true,
			"body": ` + iterBody + `
		}
	}`

	n, err := node.UnmarshalNode([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, spi.KindLoop, n.GetType())
	assert.Equal(t, spi.RunWhenOnSuccess, n.GetRunWhen())

	loopNode, ok := node.AsLoopNode(n)
	require.True(t, ok)
	data := loopNode.GetData()
	assert.Equal(t, "{{{prev.ids}}}", data.Items)
	assert.Equal(t, "id", data.ItemVar)
	assert.Equal(t, 10, data.MaxIterations)
	assert.True(t, data.ContinueOnError)
	assert.NotEmpty(t, data.Body)

	assert.Equal(t, []string{"prev.ids"}, loopNode.InputSchema())
	assert.Equal(t, []string{"results", "count"}, loopNode.OutputSchema())
}
