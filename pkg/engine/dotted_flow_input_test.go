package engine_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A launch-injected input such as webhook.url contains a dot, but no node is
// named "webhook", so the engine reads it from the flow inputs.
func TestDottedFlowInputResolvesFromTheFlowInputs(t *testing.T) {
	n := newSetVariableAssertionNode(
		map[string]any{"target": "{{webhook.url}}"},
		[]node.CompositeAssertion{jsonPathEquals(t, "$.target", "https://hooks.example/webhook/abc")},
	)

	flowInstance := flow.Flow{Name: "f", Nodes: []node.AnyNode{n}, Version: "1.0"}
	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]any{"webhook.url": "https://hooks.example/webhook/abc"})
	require.NoError(t, err)
	require.True(t, result.Success)
	assert.Equal(t, "https://hooks.example/webhook/abc", result.ExecutionResults["set1"].GetOutputs()["target"])
}
