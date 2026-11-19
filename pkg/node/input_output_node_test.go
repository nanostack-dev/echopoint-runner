package node_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFromJSON_RejectsInputAndOutputNodesByDefault(t *testing.T) {
	withInput := []byte(`{
		"name": "test-with-input",
		"nodes": [
			{
				"id": "input",
				"type": "input",
				"data": {
					"properties": {
						"order_id": {"type": "string"}
					}
				}
			}
		],
		"edges": []
	}`)

	_, err := flow.ParseFromJSON(withInput)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input node is not allowed")

	withOutput := []byte(`{
		"name": "test-with-output",
		"nodes": [
			{
				"id": "output",
				"type": "output",
				"data": {
					"properties": {
						"refund_id": {"type": "string", "value": "re_123"}
					}
				}
			}
		],
		"edges": []
	}`)

	_, err = flow.ParseFromJSON(withOutput)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output node is not allowed")
}

func TestParseFromJSONWithOptions_ValidatesInputAndOutputNodeReferences(t *testing.T) {
	validFunction := []byte(`{
		"name": "refund-function",
		"nodes": [
			{
				"id": "input",
				"type": "input",
				"data": {
					"required": ["order_id"],
					"properties": {
						"order_id": {"type": "string", "description": "Order ID"}
					}
				}
			},
			{
				"id": "output",
				"type": "output",
				"data": {
					"properties": {
						"confirmed_order": {"type": "string", "value": "{{input.order_id}}"}
					}
				}
			}
		],
		"edges": [
			{"id": "e1", "source": "input", "target": "output", "type": "success"}
		]
	}`)

	parsed, err := flow.ParseFromJSONWithOptions(validFunction, flow.ParseOptions{
		AllowInputNodes:  true,
		AllowOutputNodes: true,
	})
	require.NoError(t, err)
	require.Len(t, parsed.Nodes, 2)

	// Referencing an undeclared property on the input node fails parse-time validation.
	invalidRef := []byte(`{
		"name": "broken-function",
		"nodes": [
			{
				"id": "input",
				"type": "input",
				"data": {
					"properties": {
						"order_id": {"type": "string"}
					}
				}
			},
			{
				"id": "output",
				"type": "output",
				"data": {
					"properties": {
						"confirmed_order": {"type": "string", "value": "{{input.missing_field}}"}
					}
				}
			}
		],
		"edges": [
			{"id": "e1", "source": "input", "target": "output", "type": "success"}
		]
	}`)

	_, err = flow.ParseFromJSONWithOptions(invalidRef, flow.ParseOptions{
		AllowInputNodes:  true,
		AllowOutputNodes: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing_field")
}

func TestInputAndOutputNodes_ExecuteEndToEndWithValidationDefaultsAndRedaction(t *testing.T) {
	functionJSON := []byte(`{
		"name": "provision-function",
		"nodes": [
			{
				"id": "input",
				"type": "input",
				"data": {
					"required": ["email", "seats"],
					"properties": {
						"email": {"type": "string", "format": "email"},
						"seats": {"type": "integer"},
						"plan": {"type": "string", "enum": ["starter", "pro"], "default": "pro"},
						"api_secret": {"type": "string", "secret": true, "default": "sec_live_998877"}
					}
				}
			},
			{
				"id": "output",
				"type": "output",
				"data": {
					"properties": {
						"summary": {"type": "string", "value": "{{input.email}}:{{input.plan}}"},
						"seats_allocated": {"type": "integer", "value": "{{{input.seats}}}"},
						"echoed_secret": {"type": "string", "value": "{{input.api_secret}}"}
					}
				}
			}
		],
		"edges": [
			{"id": "e1", "source": "input", "target": "output", "type": "success"}
		]
	}`)

	parsed, err := flow.ParseFromJSONWithOptions(functionJSON, flow.ParseOptions{
		AllowInputNodes:  true,
		AllowOutputNodes: true,
	})
	require.NoError(t, err)

	// 1. Valid execution with default plan and secret input masking.
	res, err := runner.Run(*parsed, map[string]any{
		"email": "alice@nanostack.dev",
		"seats": 5,
	})
	require.NoError(t, err)
	require.True(t, res.Success)
	assert.Equal(t, "alice@nanostack.dev:pro", res.FinalOutputs["output.summary"])
	assert.Equal(t, json.Number("5"), res.FinalOutputs["output.seats_allocated"])
	assert.Equal(t, "***", res.FinalOutputs["output.echoed_secret"])
	assert.Equal(t, "***", res.FinalOutputs["input.api_secret"])

	// 2. Missing required input fails with INPUT_REQUIRED.
	resMissing, err := runner.Run(*parsed, map[string]any{
		"seats": 5,
	})
	require.Error(t, err)
	require.NotNil(t, resMissing)
	assert.Equal(t, "INPUT_REQUIRED", wireErrorCode(t, resMissing.ExecutionResults["input"]))

	// 3. Invalid type or format fails with INPUT_INVALID.
	resBadType, err := runner.Run(*parsed, map[string]any{
		"email": "not-an-email",
		"seats": 5,
	})
	require.Error(t, err)
	require.NotNil(t, resBadType)
	assert.Equal(t, "INPUT_INVALID", wireErrorCode(t, resBadType.ExecutionResults["input"]))

	// 4. Calling as a Module (Function call) without manual output_bindings auto-exports the output node's fields.
	parentJSON := []byte(`{
		"name": "test-calling-function",
		"nodes": [
			{
				"id": "call_provision",
				"type": "module",
				"data": {
					"flow_id": "fn-provision",
					"input_bindings": {
						"email": "bob@nanostack.dev",
						"seats": 10
					}
				}
			}
		],
		"edges": []
	}`)

	parentParsed, err := flow.ParseFromJSON(parentJSON)
	require.NoError(t, err)

	parentRes, err := engine.ExecuteFlowDefinition(*parentParsed, map[string]any{}, &engine.Options{
		ModuleResolver: staticResolver{
			"fn-provision": spi.ResolvedModuleFlow{
				FlowDefinition: functionJSON,
			},
		},
	})
	require.NoError(t, err)
	require.True(t, parentRes.Success)
	assert.Equal(t, "bob@nanostack.dev:pro", parentRes.FinalOutputs["call_provision.summary"])
}

type staticResolver map[string]spi.ResolvedModuleFlow

func (s staticResolver) ResolveFlow(flowID string) (spi.ResolvedModuleFlow, bool) {
	resolved, ok := s[flowID]
	return resolved, ok
}

func wireErrorCode(t *testing.T, res spi.AnyResult) string {
	t.Helper()
	encoded, err := json.Marshal(res)
	require.NoError(t, err)
	var wire struct {
		ErrorCode string `json:"error_code"`
	}
	require.NoError(t, json.Unmarshal(encoded, &wire))
	return wire.ErrorCode
}
