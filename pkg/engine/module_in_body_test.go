package engine_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const greetModule = `{
	"name": "greet", "version": "1.0",
	"nodes": [{"id": "say", "type": "set_variable",
		"data": {"variables": {"greeting": "hi {{name}}"}}}],
	"edges": []
}`

func TestModuleInsideALoopBodyRuns(t *testing.T) {
	parsed, err := flow.ParseFromJSON([]byte(`{
		"name": "parent", "version": "1.0",
		"nodes": [{"id": "loop", "type": "loop", "data": {
			"items": ["ann"],
			"body": {"nodes": [{"id": "m", "type": "module", "data": {
				"flow_id": "greet", "input_bindings": {"name": "{{item}}"},
				"output_bindings": {"greeting": "say.greeting"}}}], "edges": []}
		}}],
		"edges": []
	}`))
	require.NoError(t, err)

	result, err := engine.ExecuteFlowDefinition(*parsed, map[string]any{}, &engine.Options{
		ModuleResolver: staticModuleResolver{"greet": {FlowDefinition: []byte(greetModule)}},
	})

	require.NoError(t, err)
	require.True(t, result.Success)
	assert.Equal(t, []map[string]any{{"m.greeting": "hi ann"}},
		result.ExecutionResults["loop"].GetOutputs()["results"])
}

func TestModuleInsideAPollBodyRuns(t *testing.T) {
	parsed, err := flow.ParseFromJSON([]byte(`{
		"name": "parent", "version": "1.0",
		"nodes": [{"id": "poll", "type": "poll", "data": {
			"max_attempts": 1, "interval_ms": 1,
			"body": {"nodes": [{"id": "m", "type": "module", "data": {
				"flow_id": "greet", "input_bindings": {"name": "bob"},
				"output_bindings": {"greeting": "say.greeting"}}}], "edges": []}
			},
			"assertions": [{"extractor_type": "json_path", "extractor_data": {"path": "$['m.greeting']"},
				"operator_type": "equals", "operator_data": {"value": "hi bob"}}]
		}],
		"edges": []
	}`))
	require.NoError(t, err)

	result, err := runner.Run(*parsed, map[string]any{}, runner.WithReferencedFlows(
		flow.ReferencedFlowRegistry{"greet": {FlowDefinition: []byte(greetModule)}}))

	require.NoError(t, err)
	assert.True(t, result.Success, "%v", result.ErrorMsg)
}

// A run that carries no referenced flows names the module flow it is missing.
func TestModuleInsideALoopBodyWithoutReferencedFlowsNamesTheMissingFlow(t *testing.T) {
	parsed, err := flow.ParseFromJSON([]byte(`{
		"name": "parent", "version": "1.0",
		"nodes": [{"id": "loop", "type": "loop", "data": {
			"items": ["ann"],
			"body": {"nodes": [{"id": "m", "type": "module", "data": {"flow_id": "greet"}}], "edges": []}
		}}],
		"edges": []
	}`))
	require.NoError(t, err)

	result, err := runner.Run(*parsed, map[string]any{})

	require.Error(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, err.Error(), `referenced flow "greet" not found`)
}
