package engine_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonPathEquals builds a jsonPath-extractor equals-assertion. It is local to
// this file so the set-variable engine tests stay self-contained.
func jsonPathEquals(t *testing.T, path string, value any) node.CompositeAssertion {
	t.Helper()
	vb, err := json.Marshal(value)
	require.NoError(t, err)
	raw := `{"extractor_type":"jsonPath","extractor_data":{"path":"` + path + `"},` +
		`"operator_type":"equals","operator_data":{"value":` + string(vb) + `}}`
	var ca node.CompositeAssertion
	require.NoError(t, json.Unmarshal([]byte(raw), &ca))
	return ca
}

// newSetVariableAssertionNode builds a production SetVariableNode carrying the
// given assertions. Its assertions are evaluated by the uniform engine pass
// against the computed variables map (the node Outputs), via
// SetVariableExecutionResult.AssertionContext — set-variable runs NO assertions
// itself.
func newSetVariableAssertionNode(
	vars map[string]any, assertions []node.CompositeAssertion,
) *node.SetVariableNode {
	return &node.SetVariableNode{
		BaseNode: node.BaseNode{
			ID:          "set1",
			DisplayName: "Set Variables",
			NodeType:    node.TypeSetVariable,
			Assertions:  assertions,
		},
		Data: node.SetVariableData{Variables: vars},
	}
}

// A set_variable node with a passing assertion over its computed variables
// succeeds through the engine, with AssertionResults filled by the engine pass.
func TestSetVariableEnginePass_AssertionPasses(t *testing.T) {
	n := newSetVariableAssertionNode(
		map[string]any{"label": "{{id}}-{{name}}"},
		[]node.CompositeAssertion{jsonPathEquals(t, "$.label", "1-a")},
	)

	flowInstance := flow.Flow{Name: "f", Nodes: []node.AnyNode{n}, Version: "1.0"}
	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]any{"id": 1, "name": "a"})
	require.NoError(t, err)
	require.True(t, result.Success)

	res := result.ExecutionResults["set1"]
	require.NotNil(t, res)
	assert.Equal(t, "1-a", res.GetOutputs()["label"])

	sv, ok := res.(*node.SetVariableExecutionResult)
	require.True(t, ok)
	require.Len(t, sv.AssertionResults, 1, "engine pass filled assertion results")
	assert.True(t, sv.AssertionResults[0].Passed)
}

// A set_variable node with a failing assertion over its computed variables is
// flipped to failed by the engine pass, failing the flow.
func TestSetVariableEnginePass_AssertionFails(t *testing.T) {
	n := newSetVariableAssertionNode(
		map[string]any{"label": "{{id}}-{{name}}"},
		[]node.CompositeAssertion{jsonPathEquals(t, "$.label", "WRONG")},
	)

	flowInstance := flow.Flow{Name: "f", Nodes: []node.AnyNode{n}, Version: "1.0"}
	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]any{"id": 1, "name": "a"})
	require.Error(t, err, "a failing assertion fails the flow")
	require.False(t, result.Success)

	sv, ok := result.ExecutionResults["set1"].(*node.SetVariableExecutionResult)
	require.True(t, ok)
	require.Len(t, sv.AssertionResults, 1)
	assert.False(t, sv.AssertionResults[0].Passed)
	require.Error(t, sv.GetError())
	require.NotNil(t, sv.ErrorCode)
	assert.Equal(t, "ASSERTION_FAILED", *sv.ErrorCode)
}
