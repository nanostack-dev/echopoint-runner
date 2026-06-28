package node_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The assert node is thin: Execute resolves its target and exposes it as a
// ResponseContext; the ENGINE-level pass evaluates assertions/outputs and flips
// the result to failed on a miss. These tests therefore drive assertions through
// the engine (flow.ParseFromJSONWithOptions + engine.ExecuteFlowDefinition)
// rather than calling Execute() directly, since assertions no longer run there.

// runAssertFlow parses a single-node assert flow and executes it through the
// engine, returning the flow result, the assert node's typed result, and the
// execution error (non-nil when an assertion fails).
func runAssertFlow(
	t *testing.T, flowJSON []byte, inputKeys []string, inputs map[string]any,
) (*node.FlowExecutionResult, *node.AssertExecutionResult, error) {
	t.Helper()
	parsed, err := flow.ParseFromJSONWithOptions(flowJSON, flow.ParseOptions{
		AllowedInitialInputKeys: inputKeys,
	})
	require.NoError(t, err)

	result, execErr := engine.ExecuteFlowDefinition(*parsed, inputs, &engine.ExecuteOptions{})
	require.NotNil(t, result)
	assertRes := node.MustAs[*node.AssertExecutionResult](result.ExecutionResults["verify"])
	return result, assertRes, execErr
}

func TestAssertNode_JSONPathEqualsPasses(t *testing.T) {
	flowJSON := []byte(`{
		"name": "assert pass",
		"version": "1.0",
		"nodes": [{
			"id": "verify",
			"display_name": "Validate status",
			"type": "assert",
			"data": {},
			"assertions": [{
				"extractor_type": "jsonPath",
				"extractor_data": {"path": "$.status"},
				"operator_type": "equals",
				"operator_data": {"value": "ok"}
			}]
		}],
		"edges": []
	}`)

	result, assertRes, err := runAssertFlow(
		t, flowJSON, []string{"status"}, map[string]any{"status": "ok"},
	)
	require.NoError(t, err)
	require.True(t, result.Success)

	require.Len(t, assertRes.AssertionResults, 1)
	assert.True(t, assertRes.AssertionResults[0].Passed)
	assert.Equal(t, 0, assertRes.AssertionResults[0].Index)
	require.NoError(t, assertRes.GetError())
}

func TestAssertNode_FailingAssertionFlipsToFailed(t *testing.T) {
	flowJSON := []byte(`{
		"name": "assert fail",
		"version": "1.0",
		"nodes": [{
			"id": "verify",
			"display_name": "Validate status",
			"type": "assert",
			"data": {},
			"assertions": [{
				"extractor_type": "jsonPath",
				"extractor_data": {"path": "$.status"},
				"operator_type": "equals",
				"operator_data": {"value": "ok"}
			}]
		}],
		"edges": []
	}`)

	result, assertRes, err := runAssertFlow(
		t, flowJSON, []string{"status"}, map[string]any{"status": "error"},
	)
	require.Error(t, err)
	require.False(t, result.Success)
	assert.Contains(t, err.Error(), "assertion 0 failed")

	require.Len(t, assertRes.AssertionResults, 1)
	assert.False(t, assertRes.AssertionResults[0].Passed)
	assert.Equal(t, "error", assertRes.AssertionResults[0].Actual)
	require.NotNil(t, assertRes.ErrorMsg)
	assert.Contains(t, *assertRes.ErrorMsg, "assertion 0 failed")
	require.NotNil(t, assertRes.ErrorCode)
	// The engine-level assertion pass owns failure classification — assert misses
	// surface the unified ASSERTION_FAILED code (was node-local ASSERT_FAILED).
	assert.Equal(t, "ASSERTION_FAILED", *assertRes.ErrorCode)
}

func TestAssertNode_StopsAtFirstFailingAssertion(t *testing.T) {
	flowJSON := []byte(`{
		"name": "assert stop",
		"version": "1.0",
		"nodes": [{
			"id": "verify",
			"display_name": "Validate status",
			"type": "assert",
			"data": {},
			"assertions": [
				{
					"extractor_type": "jsonPath",
					"extractor_data": {"path": "$.status"},
					"operator_type": "equals",
					"operator_data": {"value": "ok"}
				},
				{
					"extractor_type": "jsonPath",
					"extractor_data": {"path": "$.count"},
					"operator_type": "equals",
					"operator_data": {"value": 9}
				}
			]
		}],
		"edges": []
	}`)

	_, assertRes, err := runAssertFlow(
		t, flowJSON, []string{"status", "count"},
		map[string]any{"status": "ok", "count": float64(3)},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assertion 1 failed")

	require.Len(t, assertRes.AssertionResults, 2, "should record up to and including the failing assertion")
	assert.True(t, assertRes.AssertionResults[0].Passed)
	assert.False(t, assertRes.AssertionResults[1].Passed)
	assert.Equal(t, 1, assertRes.AssertionResults[1].Index)
}

// TestAssertNode_TargetOmittedAssertsOverFlowInputs documents the omitted-target
// default: with no Data.Target, the node asserts over ctx.FlowInputs (the flow's
// effective initial inputs), NOT ctx.Inputs. The engine only populates ctx.Inputs
// from refs declared in InputSchema — which an omitted target leaves empty — so
// FlowInputs is the real map to assert over by default.
func TestAssertNode_TargetOmittedAssertsOverFlowInputs(t *testing.T) {
	flowJSON := []byte(`{
		"name": "assert flow inputs",
		"version": "1.0",
		"nodes": [{
			"id": "verify",
			"display_name": "Validate flow inputs",
			"type": "assert",
			"data": {},
			"assertions": [{
				"extractor_type": "jsonPath",
				"extractor_data": {"path": "$.userId"},
				"operator_type": "equals",
				"operator_data": {"value": "u-123"}
			}]
		}],
		"edges": []
	}`)

	result, assertRes, err := runAssertFlow(
		t, flowJSON, []string{"userId"}, map[string]any{"userId": "u-123"},
	)
	require.NoError(t, err)
	require.True(t, result.Success)

	require.Len(t, assertRes.AssertionResults, 1)
	assert.True(t, assertRes.AssertionResults[0].Passed)
}

// TestAssertNode_CapturesExtractorOutputOnSuccess verifies the engine pass also
// runs the node's declared OUTPUT extractors against the resolved target and
// merges them onto the result.
func TestAssertNode_CapturesExtractorOutputOnSuccess(t *testing.T) {
	flowJSON := []byte(`{
		"name": "assert outputs",
		"version": "1.0",
		"nodes": [{
			"id": "verify",
			"display_name": "Validate and capture",
			"type": "assert",
			"data": {},
			"assertions": [{
				"extractor_type": "jsonPath",
				"extractor_data": {"path": "$.status"},
				"operator_type": "equals",
				"operator_data": {"value": "active"}
			}],
			"outputs": [{
				"name": "userId",
				"extractor": {"type": "jsonPath", "path": "$.id"}
			}]
		}],
		"edges": []
	}`)

	result, assertRes, err := runAssertFlow(
		t, flowJSON, []string{"status", "id"},
		map[string]any{"status": "active", "id": "u-42"},
	)
	require.NoError(t, err)
	require.True(t, result.Success)

	assert.Equal(t, "u-42", assertRes.Outputs["userId"])
	require.Len(t, assertRes.AssertionResults, 1)
	assert.True(t, assertRes.AssertionResults[0].Passed)
}

// TestAssertNode_DecodeViaUnmarshalNode is a pure node-level test: it checks wire
// decoding, run_when default, InputSchema ref surfacing, and the registered
// skipped-result factory — none of which depend on assertion evaluation.
func TestAssertNode_DecodeViaUnmarshalNode(t *testing.T) {
	wire := `{
		"id": "assert-decode",
		"type": "assert",
		"display_name": "Decoded assert",
		"data": {"target": "{{{flow.body}}}"},
		"assertions": [{
			"extractor_type": "jsonPath",
			"extractor_data": {"path": "$.ok"},
			"operator_type": "equals",
			"operator_data": {"value": true}
		}]
	}`

	n, err := node.UnmarshalNode([]byte(wire))
	require.NoError(t, err)
	assert.Equal(t, node.TypeAssert, n.GetType())
	assert.Equal(t, node.RunWhenOnSuccess, n.GetRunWhen(), "run_when defaults to on_success")

	assertNode, ok := node.AsAssertNode(n)
	require.True(t, ok)
	require.Len(t, assertNode.GetAssertions(), 1)

	// target references flow.body — InputSchema must surface it.
	assert.Equal(t, []string{"flow.body"}, assertNode.InputSchema())

	// Skipped result factory is registered for the kind.
	skipped, ok := node.NewSkippedResult(node.TypeAssert, node.BaseExecutionResult{NodeID: "assert-decode"})
	require.True(t, ok)
	_, ok = node.As[*node.AssertExecutionResult](skipped)
	assert.True(t, ok)
}
