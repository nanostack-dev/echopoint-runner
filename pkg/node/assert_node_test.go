package node_test

import (
	"encoding/json"
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustAssertion decodes a single CompositeAssertion from its snake_case wire shape.
func mustAssertion(t *testing.T, wire string) node.CompositeAssertion {
	t.Helper()
	var ca node.CompositeAssertion
	require.NoError(t, json.Unmarshal([]byte(wire), &ca), "decode assertion")
	return ca
}

// jsonPathEquals builds an assertion that asserts a JSONPath equals a value.
func jsonPathEquals(t *testing.T, path string, expected any) node.CompositeAssertion {
	t.Helper()
	exp, err := json.Marshal(expected)
	require.NoError(t, err)
	wire := `{
		"extractor_type": "jsonPath",
		"extractor_data": {"path": "` + path + `"},
		"operator_type": "equals",
		"operator_data": {"value": ` + string(exp) + `}
	}`
	return mustAssertion(t, wire)
}

func TestAssertNode_JSONPathEqualsPasses(t *testing.T) {
	n := &node.AssertNode{
		BaseNode: node.BaseNode{
			ID:          "assert1",
			DisplayName: "Validate status",
			NodeType:    node.TypeAssert,
			Assertions: []node.CompositeAssertion{
				jsonPathEquals(t, "$.status", "ok"),
			},
		},
		Data: node.AssertData{
			Target: map[string]any{"status": "ok", "count": float64(3)},
		},
	}

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)

	assertRes, ok := node.As[*node.AssertExecutionResult](res)
	require.True(t, ok, "result should be *AssertExecutionResult")
	require.Len(t, assertRes.AssertionResults, 1)
	assert.True(t, assertRes.AssertionResults[0].Passed)
	assert.Equal(t, 0, assertRes.AssertionResults[0].Index)
	require.NoError(t, assertRes.GetError())
}

func TestAssertNode_FailingAssertionReturnsError(t *testing.T) {
	n := &node.AssertNode{
		BaseNode: node.BaseNode{
			ID:          "assert1",
			DisplayName: "Validate status",
			NodeType:    node.TypeAssert,
			Assertions: []node.CompositeAssertion{
				jsonPathEquals(t, "$.status", "ok"),
			},
		},
		Data: node.AssertData{
			Target: map[string]any{"status": "error"},
		},
	}

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assertion 0 failed")

	assertRes, ok := node.As[*node.AssertExecutionResult](res)
	require.True(t, ok)
	require.Len(t, assertRes.AssertionResults, 1)
	assert.False(t, assertRes.AssertionResults[0].Passed)
	assert.Equal(t, "error", assertRes.AssertionResults[0].Actual)
	require.NotNil(t, assertRes.ErrorMsg)
	assert.Contains(t, *assertRes.ErrorMsg, "assertion 0 failed")
	require.NotNil(t, assertRes.ErrorCode)
	assert.Equal(t, "ASSERT_FAILED", *assertRes.ErrorCode)
}

func TestAssertNode_StopsAtFirstFailingAssertion(t *testing.T) {
	n := &node.AssertNode{
		BaseNode: node.BaseNode{
			ID:          "assert1",
			DisplayName: "Validate status",
			NodeType:    node.TypeAssert,
			Assertions: []node.CompositeAssertion{
				jsonPathEquals(t, "$.status", "ok"),      // passes
				jsonPathEquals(t, "$.count", float64(9)), // fails (actual 3)
			},
		},
		Data: node.AssertData{
			Target: map[string]any{"status": "ok", "count": float64(3)},
		},
	}

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assertion 1 failed")

	assertRes, ok := node.As[*node.AssertExecutionResult](res)
	require.True(t, ok)
	require.Len(t, assertRes.AssertionResults, 2, "should record up to and including the failing assertion")
	assert.True(t, assertRes.AssertionResults[0].Passed)
	assert.False(t, assertRes.AssertionResults[1].Passed)
	assert.Equal(t, 1, assertRes.AssertionResults[1].Index)
}

func TestAssertNode_TargetOmittedAssertsOverInputs(t *testing.T) {
	n := &node.AssertNode{
		BaseNode: node.BaseNode{
			ID:          "assert1",
			DisplayName: "Validate inputs",
			NodeType:    node.TypeAssert,
			Assertions: []node.CompositeAssertion{
				jsonPathEquals(t, "$['create-user.userId']", "u-123"),
			},
		},
		// No Data.Target — asserts over ctx.Inputs.
	}

	res, err := n.Execute(node.ExecutionContext{
		Inputs: map[string]any{"create-user.userId": "u-123"},
	})
	require.NoError(t, err)

	assertRes := node.MustAs[*node.AssertExecutionResult](res)
	require.Len(t, assertRes.AssertionResults, 1)
	assert.True(t, assertRes.AssertionResults[0].Passed)
}

func TestAssertNode_CapturesExtractorOutputOnSuccess(t *testing.T) {
	outputWire := `{
		"id": "assert1",
		"type": "assert",
		"display_name": "Validate and capture",
		"data": {"target": "{{{user.payload}}}"},
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
	}`

	anyNode, err := node.UnmarshalNode([]byte(outputWire))
	require.NoError(t, err)
	assertNode, ok := node.AsAssertNode(anyNode)
	require.True(t, ok)

	res, execErr := assertNode.Execute(node.ExecutionContext{
		Inputs: map[string]any{
			"user.payload": map[string]any{"status": "active", "id": "u-42"},
		},
	})
	require.NoError(t, execErr)

	assertRes := node.MustAs[*node.AssertExecutionResult](res)
	assert.Equal(t, "u-42", assertRes.Outputs["userId"])
	require.Len(t, assertRes.AssertionResults, 1)
	assert.True(t, assertRes.AssertionResults[0].Passed)
}

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
