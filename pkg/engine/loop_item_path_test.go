package engine_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeFlowJSON(t *testing.T, flowJSON string, inputs map[string]any) map[string]any {
	t.Helper()
	parsed, err := flow.ParseFromJSONWithOptions([]byte(flowJSON), flow.ParseOptions{})
	require.NoError(t, err)
	flowEngine, err := engine.NewFlowEngine(*parsed, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(inputs)
	require.NoError(t, err)
	require.True(t, result.Success)
	outputs := make(map[string]any, len(result.ExecutionResults))
	for nodeID, nodeResult := range result.ExecutionResults {
		outputs[nodeID] = nodeResult.GetOutputs()
	}
	return outputs
}

// A loop body reads a field of the current item with {{item.id}}, and the loop
// takes its array from a plain {{accounts}} reference.
func TestLoopBodyResolvesAFieldOfTheCurrentItem(t *testing.T) {
	outputs := executeFlowJSON(t, `{
		"name": "f", "version": "1.0",
		"nodes": [{
			"id": "loop", "type": "loop",
			"data": {
				"items": "{{accounts}}",
				"body": {"nodes": [{"id": "pick", "type": "set_variable",
					"data": {"variables": {"account_id": "{{item.id}}"}}}], "edges": []}
			}
		}],
		"edges": [],
		"initial_inputs": {"accounts": []}
	}`, map[string]any{"accounts": []any{map[string]any{"id": "a1"}, map[string]any{"id": "b2"}}})

	assert.Equal(t, []map[string]any{
		{"pick.account_id": "a1"},
		{"pick.account_id": "b2"},
	}, outputs["loop"].(map[string]any)["results"])
}

func TestNodeOutputPathResolvesIntoAnUpstreamArray(t *testing.T) {
	outputs := executeFlowJSON(t, `{
		"name": "f", "version": "1.0",
		"nodes": [
			{"id": "list_accounts", "type": "set_variable",
				"data": {"variables": {"accounts": "{{{accounts}}}"}}},
			{"id": "pick", "type": "set_variable",
				"data": {"variables": {"first_id": "{{list_accounts.accounts.0.id}}"}}}
		],
		"edges": [{"id": "e1", "source": "list_accounts", "target": "pick"}],
		"initial_inputs": {"accounts": []}
	}`, map[string]any{"accounts": []any{map[string]any{"id": "a1"}}})

	assert.Equal(t, "a1", outputs["pick"].(map[string]any)["first_id"])
}
