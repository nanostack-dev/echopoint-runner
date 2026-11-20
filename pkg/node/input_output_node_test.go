package node_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
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

func TestParseFromJSONWithOptions_RejectsDuplicateContractNodesAndMissingOutputValue(t *testing.T) {
	twoInputs := []byte(`{
		"name": "two-inputs",
		"nodes": [
			{"id": "in1", "type": "input", "data": {"properties": {"a": {"type": "string"}}}},
			{"id": "in2", "type": "input", "data": {"properties": {"b": {"type": "string"}}}}
		],
		"edges": []
	}`)
	_, err := flow.ParseFromJSONWithOptions(twoInputs, flow.ParseOptions{AllowInputNodes: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most one input node")

	twoOutputs := []byte(`{
		"name": "two-outputs",
		"nodes": [
			{"id": "out1", "type": "output", "data": {"properties": {"a": {"type": "string", "value": "x"}}}},
			{"id": "out2", "type": "output", "data": {"properties": {"b": {"type": "string", "value": "y"}}}}
		],
		"edges": []
	}`)
	_, err = flow.ParseFromJSONWithOptions(twoOutputs, flow.ParseOptions{AllowOutputNodes: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most one output node")

	missingOutputValue := []byte(`{
		"name": "missing-output-value",
		"nodes": [
			{"id": "output", "type": "output", "data": {"properties": {"a": {"type": "string"}}}}
		],
		"edges": []
	}`)
	_, err = flow.ParseFromJSONWithOptions(missingOutputValue, flow.ParseOptions{AllowOutputNodes: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `requires a value`)

	// Direct execution of an OutputNode constructed without a value returns OUTPUT_VALUE_MISSING.
	directOut := &node.OutputNode{
		BaseNode: node.BaseNode{ID: "output", NodeType: spi.KindOutput},
		Data: node.OutputData{
			Properties: map[string]node.OutputProperty{
				"a": {Type: "string"},
			},
		},
	}
	res, execErr := directOut.Execute(spi.ExecutionContext{})
	require.Error(t, execErr)
	assert.Equal(t, "OUTPUT_VALUE_MISSING", wireErrorCode(t, res))
}

func TestParseFromJSON_RejectsInputAndOutputNodesInsideLoopAndPollBodies(t *testing.T) {
	loopWithInput := []byte(`{
		"name": "loop-bypass",
		"nodes": [
			{
				"id": "loop1",
				"type": "loop",
				"data": {
					"items": ["a"],
					"body": {
						"nodes": [
							{"id": "in1", "type": "input", "data": {"properties": {"x": {"type": "string"}}}}
						],
						"edges": []
					}
				}
			}
		],
		"edges": []
	}`)
	_, err := flow.ParseFromJSON(loopWithInput)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed inside a loop or poll body")

	pollWithOutput := []byte(`{
		"name": "poll-bypass",
		"nodes": [
			{
				"id": "poll1",
				"type": "poll",
				"data": {
					"interval_ms": 10,
					"max_attempts": 1,
					"body": {
						"nodes": [
							{"id": "out1", "type": "output", "data": {"properties": {"x": {"type": "string", "value": "ok"}}}}
						],
						"edges": []
					}
				}
			}
		],
		"edges": []
	}`)
	_, err = flow.ParseFromJSON(pollWithOutput)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed inside a loop or poll body")
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

	// 4. Parent flow calling the function without manual output_bindings can
	// reference auto-exported outputs directly in downstream nodes.
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
			},
			{
				"id": "use_summary",
				"type": "set_variable",
				"data": {
					"variables": {
						"greeting": "Provisioned {{call_provision.summary}}"
					}
				}
			}
		],
		"edges": [
			{"id": "e1", "source": "call_provision", "target": "use_summary", "type": "success"}
		]
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
	assert.Equal(t, "Provisioned bob@nanostack.dev:pro", parentRes.FinalOutputs["use_summary.greeting"])

	// 5. Referencing an undeclared output on the called function fails validation cleanly.
	badParentJSON := []byte(`{
		"name": "test-calling-missing-output",
		"nodes": [
			{
				"id": "call_provision",
				"type": "module",
				"data": {
					"flow_id": "fn-provision",
					"input_bindings": {"email": "bob@nanostack.dev", "seats": 10}
				}
			},
			{
				"id": "use_missing",
				"type": "set_variable",
				"data": {
					"variables": {"bad": "{{call_provision.does_not_exist}}"}
				}
			}
		],
		"edges": [
			{"id": "e1", "source": "call_provision", "target": "use_missing", "type": "success"}
		]
	}`)
	badParentParsed, err := flow.ParseFromJSON(badParentJSON)
	require.NoError(t, err)

	_, err = engine.ExecuteFlowDefinition(*badParentParsed, map[string]any{}, &engine.Options{
		ModuleResolver: staticResolver{
			"fn-provision": spi.ResolvedModuleFlow{FlowDefinition: functionJSON},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does_not_exist")
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
