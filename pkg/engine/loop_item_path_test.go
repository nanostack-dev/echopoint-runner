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

// A child module reads a field of an input it only receives through the
// module's bindings; prevalidation must know the bound key.
func TestModuleChildReadsAPathIntoABoundInput(t *testing.T) {
	child := `{
		"name": "child", "version": "1.0",
		"nodes": [{"id": "pick", "type": "set_variable",
			"data": {"variables": {"first_id": "{{account.id}}"}}}],
		"edges": []
	}`
	parent, err := flow.ParseFromJSONWithOptions([]byte(`{
		"name": "parent", "version": "1.0",
		"nodes": [{"id": "mod", "type": "module",
			"data": {"flow_id": "child", "input_bindings": {"account": "{{{account}}}"}}}],
		"edges": [],
		"initial_inputs": {"account": {}}
	}`), flow.ParseOptions{})
	require.NoError(t, err)
	flowEngine, err := engine.NewFlowEngine(*parent, &engine.Options{
		ModuleResolver: staticModuleResolver{"child": {FlowDefinition: []byte(child)}},
	})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]any{"account": map[string]any{"id": "a1"}})

	require.NoError(t, err)
	assert.True(t, result.Success)
}

// A branch skips a step named like a flow input; a consumer reading a path
// into that input still runs.
func TestPathIntoAFlowInputIgnoresASkippedStepOfTheSameName(t *testing.T) {
	outputs := executeFlowJSON(t, `{
		"name": "f", "version": "1.0",
		"nodes": [
			{"id": "router", "type": "branch", "data": {"target": "{{route}}", "cases": [
				{"when": {"extractor_type": "body", "extractor_data": {}, "operator_type": "equals",
					"operator_data": {"value": "a"}}, "target": "account"},
				{"when": {"extractor_type": "body", "extractor_data": {}, "operator_type": "equals",
					"operator_data": {"value": "b"}}, "target": "other"}]}},
			{"id": "account", "type": "set_variable", "data": {"variables": {"x": "1"}}},
			{"id": "other", "type": "set_variable", "data": {"variables": {"y": "2"}}},
			{"id": "consumer", "type": "set_variable",
				"data": {"variables": {"first_id": "{{account.id}}"}}}
		],
		"edges": [
			{"id": "e1", "source": "router", "target": "account"},
			{"id": "e2", "source": "router", "target": "other"},
			{"id": "e3", "source": "other", "target": "consumer"}
		],
		"initial_inputs": {"route": "b", "account": {}}
	}`, map[string]any{"route": "b", "account": map[string]any{"id": "a1"}})

	require.Contains(t, outputs, "consumer")
	assert.Equal(t, "a1", outputs["consumer"].(map[string]any)["first_id"])
}

// A child module reads a field of an input it inherits from its parent without
// a binding; prevalidation must count inherited inputs.
func TestModuleChildReadsAPathIntoAnInheritedInput(t *testing.T) {
	child := `{
		"name": "child", "version": "1.0",
		"nodes": [{"id": "pick", "type": "set_variable",
			"data": {"variables": {"first_id": "{{account.id}}"}}}],
		"edges": []
	}`
	parent, err := flow.ParseFromJSONWithOptions([]byte(`{
		"name": "parent", "version": "1.0",
		"nodes": [{"id": "mod", "type": "module", "data": {"flow_id": "child"}}],
		"edges": [],
		"initial_inputs": {"account": {}}
	}`), flow.ParseOptions{})
	require.NoError(t, err)

	result, err := engine.ExecuteFlowDefinition(*parent,
		map[string]any{"account": map[string]any{"id": "a1"}},
		&engine.Options{ModuleResolver: staticModuleResolver{"child": {FlowDefinition: []byte(child)}}})

	require.NoError(t, err)
	assert.True(t, result.Success)
}

// The flow input owns {{account.id}} even when the field is missing: the
// consumer fails on the missing input rather than being skipped because an
// unrelated step named account was.
func TestMissingFieldOfAFlowInputFailsInsteadOfSkipping(t *testing.T) {
	parsed, err := flow.ParseFromJSONWithOptions([]byte(`{
		"name": "f", "version": "1.0",
		"nodes": [
			{"id": "router", "type": "branch", "data": {"target": "{{route}}", "cases": [
				{"when": {"extractor_type": "body", "extractor_data": {}, "operator_type": "equals",
					"operator_data": {"value": "a"}}, "target": "account"},
				{"when": {"extractor_type": "body", "extractor_data": {}, "operator_type": "equals",
					"operator_data": {"value": "b"}}, "target": "other"}]}},
			{"id": "account", "type": "set_variable", "data": {"variables": {"x": "1"}}},
			{"id": "other", "type": "set_variable", "data": {"variables": {"y": "2"}}},
			{"id": "consumer", "type": "set_variable",
				"data": {"variables": {"first_id": "{{account.id}}"}}}
		],
		"edges": [
			{"id": "e1", "source": "router", "target": "account"},
			{"id": "e2", "source": "router", "target": "other"},
			{"id": "e3", "source": "other", "target": "consumer"}
		],
		"initial_inputs": {"route": "b", "account": {}}
	}`), flow.ParseOptions{})
	require.NoError(t, err)
	flowEngine, err := engine.NewFlowEngine(*parsed, &engine.Options{})
	require.NoError(t, err)

	result, _ := flowEngine.Execute(map[string]any{"route": "b", "account": map[string]any{}})

	assert.False(t, result.Success, "a missing input field must fail the flow, not skip its consumer")
}
