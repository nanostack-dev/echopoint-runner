package engine_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/edge"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertionResult is a minimal result that opts into the engine-level
// assertion/output pass by exposing a value-backed ResponseContext. It mirrors
// what the migrated RequestNode does: build the success result, attach the
// context, and let the engine evaluate assertions and outputs.
type assertionResult struct {
	spi.BaseExecutionResult

	ctx extractors.ResponseContext
}

func (r *assertionResult) AssertionContext() extractors.ResponseContext { return r.ctx }

// assertionNode is a test node that returns a value-backed result and carries
// assertions + outputs the engine pass evaluates. attempts counts every Execute
// call so retry behavior can be asserted.
type assertionNode struct {
	node.BaseNode

	value    any
	attempts *int
}

func (n *assertionNode) InputSchema() []string  { return nil }
func (n *assertionNode) OutputSchema() []string { return outputNames(n.Outputs) }

func (n *assertionNode) Execute(_ spi.ExecutionContext) (spi.AnyResult, error) {
	if n.attempts != nil {
		*n.attempts++
	}
	return &assertionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    n.GetType(),
			Outputs:     map[string]any{},
			ExecutedAt:  time.Now(),
		},
		ctx: extractors.NewValueResponseContext(n.value),
	}, nil
}

func outputNames(outs []node.Output) []string {
	names := make([]string, 0, len(outs))
	for _, o := range outs {
		names = append(names, o.Name)
	}
	return names
}

func mkAssertionJSON(t *testing.T, extractor, path, op string, value any) node.CompositeAssertion {
	t.Helper()
	ed := `{}`
	if path != "" {
		ed = `{"path":"` + path + `"}`
	}
	vb, err := json.Marshal(value)
	require.NoError(t, err)
	raw := `{"extractor_type":"` + extractor + `","extractor_data":` + ed +
		`,"operator_type":"` + op + `","operator_data":{"value":` + string(vb) + `}}`
	var ca node.CompositeAssertion
	require.NoError(t, json.Unmarshal([]byte(raw), &ca))
	return ca
}

func mkOutput(t *testing.T, name, path string) node.Output {
	t.Helper()
	ext, err := extractors.UnmarshalExtractor([]byte(`{"type":"jsonPath","path":"` + path + `"}`))
	require.NoError(t, err)
	return node.Output{Name: name, Extractor: ext}
}

// (a) A node with assertions + outputs goes through the engine pass and gets
// AssertionResults filled and outputs merged.
func TestEnginePass_FillsAssertionResultsAndMergesOutputs(t *testing.T) {
	attempts := 0
	n := &assertionNode{
		BaseNode: node.BaseNode{
			ID:          "step",
			DisplayName: "Step",
			NodeType:    spi.KindRequest,
			Assertions: []node.CompositeAssertion{
				mkAssertionJSON(t, "jsonPath", "$.status", "equals", "ok"),
			},
			Outputs: []node.Output{mkOutput(t, "id", "$.id")},
		},
		value:    map[string]any{"status": "ok", "id": "prd_1"},
		attempts: &attempts,
	}

	flowInstance := flow.Flow{Name: "f", Nodes: []node.AnyNode{n}, Version: "1.0"}
	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]any{})
	require.NoError(t, err)
	require.True(t, result.Success)

	res := result.ExecutionResults["step"]
	require.NotNil(t, res)
	require.Len(t, res.GetOutputs(), 1)
	assert.Equal(t, "prd_1", res.GetOutputs()["id"], "output extracted and merged by the engine pass")

	_, ok := res.(node.AssertionContextProvider)
	require.True(t, ok, "the result implements AssertionContextProvider")
	base := res.(*assertionResult)
	require.Len(t, base.AssertionResults, 1)
	assert.True(t, base.AssertionResults[0].Passed)
	assert.Equal(t, 1, attempts, "no retry middleware: a single execution")
}

// (b) An assertion failure via the engine flips the node to failed with the
// assertion results captured.
func TestEnginePass_AssertionFailureFlipsNodeToFailed(t *testing.T) {
	n := &assertionNode{
		BaseNode: node.BaseNode{
			ID:          "step",
			DisplayName: "Step",
			NodeType:    spi.KindRequest,
			Assertions: []node.CompositeAssertion{
				mkAssertionJSON(t, "jsonPath", "$.status", "equals", "ok"),
			},
		},
		value: map[string]any{"status": "nope"},
	}

	flowInstance := flow.Flow{Name: "f", Nodes: []node.AnyNode{n}, Version: "1.0"}
	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]any{})
	require.Error(t, err, "a failing assertion fails the flow")
	require.False(t, result.Success)

	res := result.ExecutionResults["step"].(*assertionResult)
	require.Len(t, res.AssertionResults, 1)
	assert.False(t, res.AssertionResults[0].Passed)
	require.Error(t, res.GetError(), "the result carries the assertion failure error")
	require.NotNil(t, res.ErrorCode)
	assert.Equal(t, "ASSERTION_FAILED", *res.ErrorCode)
}

// (c) Retry middleware still retries when an assertion fails — the pass runs
// inside the chain, so each retry re-evaluates assertions.
func TestEnginePass_RetryRetriesOnAssertionFailure(t *testing.T) {
	attempts := 0
	n := &assertionNode{
		BaseNode: node.BaseNode{
			ID:          "step",
			DisplayName: "Step",
			NodeType:    spi.KindRequest,
			Assertions: []node.CompositeAssertion{
				mkAssertionJSON(t, "jsonPath", "$.status", "equals", "ok"),
			},
		},
		value:    map[string]any{"status": "nope"}, // always fails the assertion
		attempts: &attempts,
	}

	// A retry middleware that re-runs the node up to 3 times on error. Because the
	// assertion pass is wrapped INSIDE the chain, each attempt re-evaluates the
	// (failing) assertion, so all 3 attempts run.
	retry := func(next engine.NodeExecutor) engine.NodeExecutor {
		return func(ec spi.ExecutionContext) (spi.AnyResult, error) {
			var (
				res spi.AnyResult
				err error
			)
			for range 3 {
				res, err = next(ec)
				if err == nil {
					return res, nil
				}
			}
			return res, err
		}
	}

	flowInstance := flow.Flow{Name: "f", Nodes: []node.AnyNode{n}, Version: "1.0"}
	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{
		Middleware: []engine.Middleware{retry},
	})
	require.NoError(t, err)

	_, err = flowEngine.Execute(map[string]any{})
	require.Error(t, err)
	assert.Equal(t, 3, attempts, "retry must re-run the node on assertion failure (pass is inside the chain)")
}

// A node whose result does not implement AssertionContextProvider is left
// untouched by the engine pass (delay/module parity).
func TestEnginePass_NonProviderResultUnaffected(t *testing.T) {
	flowInstance := flow.Flow{
		Name:    "f",
		Nodes:   []node.AnyNode{&MockNode{id: "m", nodeType: spi.KindDelay}},
		Version: "1.0",
	}
	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]any{})
	require.NoError(t, err)
	require.True(t, result.Success)

	_, ok := result.ExecutionResults["m"].(node.AssertionContextProvider)
	assert.False(t, ok, "the MockNode result is not a provider, so the pass is a no-op")
}

func TestEnginePass_LinearFlowEdgesUnaffected(t *testing.T) {
	// Guard: a node that provides a context but has no assertions/outputs still
	// succeeds and propagates through edges.
	a := &assertionNode{
		BaseNode: node.BaseNode{ID: "a", DisplayName: "A", NodeType: spi.KindRequest},
		value:    map[string]any{},
	}
	b := &MockNode{id: "b", nodeType: spi.KindRequest, shouldPass: true}
	flowInstance := flow.Flow{
		Name:    "f",
		Nodes:   []node.AnyNode{a, b},
		Edges:   []edge.Edge{{ID: "e1", Source: "a", Target: "b", Type: "success"}},
		Version: "1.0",
	}
	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)
	result, err := flowEngine.Execute(map[string]any{})
	require.NoError(t, err)
	require.True(t, result.Success)
	assert.True(t, b.executed)
}
