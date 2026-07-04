package node_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// branchJSON builds a branch node wire definition with the given target value,
// cases (jsonPath==value -> target), and default.
const branchWireGT = `{
  "id": "router",
  "type": "branch",
  "display_name": "Route by status",
  "data": {
    "target": "{{check.status}}",
    "cases": [
      {
        "when": {
          "extractor_type": "body",
          "extractor_data": {},
          "operator_type": "equals",
          "operator_data": {"value": "active"}
        },
        "target": "activePath"
      },
      {
        "when": {
          "extractor_type": "body",
          "extractor_data": {},
          "operator_type": "equals",
          "operator_data": {"value": "pending"}
        },
        "target": "pendingPath"
      }
    ],
    "default": "fallbackPath"
  }
}`

func decodeBranch(t *testing.T, raw string) *node.BranchNode {
	t.Helper()
	n, err := node.UnmarshalNode([]byte(raw))
	require.NoError(t, err)
	branchNode, ok := node.AsBranchNode(n)
	require.True(t, ok, "decoded node should be a *BranchNode")
	return branchNode
}

func TestBranchNode_DecodeViaUnmarshalNode(t *testing.T) {
	branchNode := decodeBranch(t, branchWireGT)
	assert.Equal(t, spi.KindBranch, branchNode.GetType())
	assert.Equal(t, spi.RunWhenOnSuccess, branchNode.GetRunWhen())
	require.Len(t, branchNode.GetData().Cases, 2)
	assert.Equal(t, "activePath", branchNode.GetData().Cases[0].Target)
	assert.Equal(t, "fallbackPath", branchNode.GetData().Default)
}

func TestBranchNode_Execute_FirstMatchingCaseWins(t *testing.T) {
	branchNode := decodeBranch(t, branchWireGT)

	result, err := branchNode.Execute(spi.ExecutionContext{
		Inputs: map[string]any{"check.status": "active"},
	})
	require.NoError(t, err)

	branchResult, ok := spi.As[*node.BranchExecutionResult](result)
	require.True(t, ok)
	assert.Equal(t, "activePath", branchResult.MatchedTarget)
	assert.Equal(t, []string{"activePath"}, branchResult.RoutedTargets())
	assert.Equal(t, "activePath", branchResult.Outputs["matched"])
	assert.Equal(t, 0, branchResult.Outputs["matchedIndex"])
}

func TestBranchNode_Execute_SecondCaseMatches(t *testing.T) {
	branchNode := decodeBranch(t, branchWireGT)

	result, err := branchNode.Execute(spi.ExecutionContext{
		Inputs: map[string]any{"check.status": "pending"},
	})
	require.NoError(t, err)

	branchResult := spi.MustAs[*node.BranchExecutionResult](result)
	assert.Equal(t, "pendingPath", branchResult.MatchedTarget)
	assert.Equal(t, []string{"pendingPath"}, branchResult.RoutedTargets())
	assert.Equal(t, 1, branchResult.Outputs["matchedIndex"])
}

func TestBranchNode_Execute_NoMatchUsesDefault(t *testing.T) {
	branchNode := decodeBranch(t, branchWireGT)

	result, err := branchNode.Execute(spi.ExecutionContext{
		Inputs: map[string]any{"check.status": "archived"},
	})
	require.NoError(t, err)

	branchResult := spi.MustAs[*node.BranchExecutionResult](result)
	assert.Equal(t, "fallbackPath", branchResult.MatchedTarget)
	assert.Equal(t, []string{"fallbackPath"}, branchResult.RoutedTargets())
	assert.Equal(t, -1, branchResult.Outputs["matchedIndex"])
}

func TestBranchNode_Execute_NoMatchNoDefaultRoutesNothing(t *testing.T) {
	const noDefault = `{
      "id": "router",
      "type": "branch",
      "data": {
        "target": "{{check.status}}",
        "cases": [
          {
            "when": {
              "extractor_type": "body",
              "extractor_data": {},
              "operator_type": "equals",
              "operator_data": {"value": "active"}
            },
            "target": "activePath"
          }
        ]
      }
    }`
	branchNode := decodeBranch(t, noDefault)

	result, err := branchNode.Execute(spi.ExecutionContext{
		Inputs: map[string]any{"check.status": "inactive"},
	})
	require.NoError(t, err)

	branchResult := spi.MustAs[*node.BranchExecutionResult](result)
	assert.Empty(t, branchResult.MatchedTarget)
	assert.Empty(t, branchResult.RoutedTargets())
	assert.Empty(t, branchResult.Outputs["matched"])
	assert.Equal(t, -1, branchResult.Outputs["matchedIndex"])
}

func TestBranchNode_Execute_JSONPathOverDefaultInputTarget(t *testing.T) {
	// No explicit target -> conditions evaluate over the map of ctx.Inputs.
	const jsonPathBranch = `{
      "id": "router",
      "type": "branch",
      "data": {
        "cases": [
          {
            "when": {
              "extractor_type": "jsonPath",
              "extractor_data": {"path": "$['user.role']"},
              "operator_type": "equals",
              "operator_data": {"value": "admin"}
            },
            "target": "adminPath"
          }
        ],
        "default": "userPath"
      }
    }`
	branchNode := decodeBranch(t, jsonPathBranch)

	adminResult := spi.MustAs[*node.BranchExecutionResult](
		mustExec(t, branchNode, map[string]any{"user.role": "admin"}),
	)
	assert.Equal(t, "adminPath", adminResult.MatchedTarget)

	userResult := spi.MustAs[*node.BranchExecutionResult](
		mustExec(t, branchNode, map[string]any{"user.role": "viewer"}),
	)
	assert.Equal(t, "userPath", userResult.MatchedTarget)
}

func TestBranchNode_Execute_NumericComparison(t *testing.T) {
	const numeric = `{
      "id": "router",
      "type": "branch",
      "data": {
        "target": "{{{order.total}}}",
        "cases": [
          {
            "when": {
              "extractor_type": "body",
              "extractor_data": {},
              "operator_type": "greaterThan",
              "operator_data": {"value": 100}
            },
            "target": "highValue"
          }
        ],
        "default": "standard"
      }
    }`
	branchNode := decodeBranch(t, numeric)

	high := spi.MustAs[*node.BranchExecutionResult](mustExec(t, branchNode, map[string]any{"order.total": 250}))
	assert.Equal(t, "highValue", high.MatchedTarget)

	low := spi.MustAs[*node.BranchExecutionResult](mustExec(t, branchNode, map[string]any{"order.total": 40}))
	assert.Equal(t, "standard", low.MatchedTarget)
}

func TestBranchExecutionResult_ImplementsRoutingResult(t *testing.T) {
	var result spi.AnyResult = &node.BranchExecutionResult{
		RoutedTargetIDs: []string{"a"},
	}
	routing, ok := result.(spi.RoutingResult)
	require.True(t, ok, "BranchExecutionResult must implement spi.RoutingResult")
	assert.Equal(t, []string{"a"}, routing.RoutedTargets())
}

func mustExec(t *testing.T, n node.AnyNode, inputs map[string]any) spi.AnyResult {
	t.Helper()
	result, err := n.Execute(spi.ExecutionContext{Inputs: inputs})
	require.NoError(t, err)
	return result
}
