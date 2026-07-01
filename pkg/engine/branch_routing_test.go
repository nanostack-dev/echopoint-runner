package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/edge"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// branchNodeJSON builds a branch node wire definition routing on the initial
// input "route": case "a" -> targetA, case "b" -> targetB, with an optional
// default. The branch target reads the initial variable {{route}}.
func branchNodeForTest(t *testing.T, id, targetA, targetB, def string) node.AnyNode {
	t.Helper()
	raw := `{
      "id": "` + id + `",
      "type": "branch",
      "display_name": "Router",
      "data": {
        "target": "{{route}}",
        "cases": [
          {
            "when": {
              "extractor_type": "body",
              "extractor_data": {},
              "operator_type": "equals",
              "operator_data": {"value": "a"}
            },
            "target": "` + targetA + `"
          },
          {
            "when": {
              "extractor_type": "body",
              "extractor_data": {},
              "operator_type": "equals",
              "operator_data": {"value": "b"}
            },
            "target": "` + targetB + `"
          }
        ]` + defaultClause(def) + `
      }
    }`
	parsed, err := node.UnmarshalNode([]byte(raw))
	require.NoError(t, err)
	return parsed
}

func defaultClause(def string) string {
	if def == "" {
		return ""
	}
	return `, "default": "` + def + `"`
}

func successEdge(source, target string) edge.Edge {
	return edge.Edge{ID: source + "->" + target, Source: source, Target: target, Type: edge.TypeSuccess}
}

// (a) branch routes to A: A executes, B (and its subtree) is skipped.
func TestBranchRouting_RoutesToChosenSuccessor(t *testing.T) {
	branch := branchNodeForTest(t, "router", "nodeA", "nodeB", "")
	nodeA := newDataContractMockNode("nodeA", nil, nil)
	nodeB := newDataContractMockNode("nodeB", nil, nil)

	flowInstance := flow.Flow{
		Name:          "branch-routes",
		Version:       "1.0",
		InitialInputs: map[string]any{"route": "a"},
		Nodes:         []node.AnyNode{branch, nodeA, nodeB},
		Edges: []edge.Edge{
			successEdge("router", "nodeA"),
			successEdge("router", "nodeB"),
		},
	}

	result, err := engine.ExecuteFlowDefinition(flowInstance, map[string]any{"route": "a"}, nil)
	require.NoError(t, err)
	require.True(t, result.Success)

	// router and nodeA ran; nodeB skipped.
	branchResult := spi.MustAs[*node.BranchExecutionResult](result.ExecutionResults["router"])
	assert.Equal(t, "nodeA", branchResult.MatchedTarget)

	assert.NotNil(t, nodeA.executedAt, "nodeA should execute")
	assert.Nil(t, nodeB.executedAt, "nodeB should NOT execute")

	skippedB := result.ExecutionResults["nodeB"]
	require.NotNil(t, skippedB)
	requireSkipped(t, skippedB)
}

// (b) diamond reconvergence: J runs exactly once with A's data when A is taken.
func TestBranchRouting_DiamondReconvergence(t *testing.T) {
	branch := branchNodeForTest(t, "router", "nodeA", "nodeB", "")
	nodeA := newDataContractMockNode("nodeA", nil, []string{"value"})
	nodeA.outputs = map[string]any{"value": "fromA"}
	nodeB := newDataContractMockNode("nodeB", nil, []string{"value"})
	nodeB.outputs = map[string]any{"value": "fromB"}
	// J consumes A's output. Because B is skipped, only A's edge feeds J.
	nodeJ := newDataContractMockNode("nodeJ", []string{"nodeA.value"}, []string{"joined"})
	nodeJ.outputs = map[string]any{"joined": "ok"}

	flowInstance := flow.Flow{
		Name:          "branch-diamond",
		Version:       "1.0",
		InitialInputs: map[string]any{"route": "a"},
		Nodes:         []node.AnyNode{branch, nodeA, nodeB, nodeJ},
		Edges: []edge.Edge{
			successEdge("router", "nodeA"),
			successEdge("router", "nodeB"),
			successEdge("nodeA", "nodeJ"),
			successEdge("nodeB", "nodeJ"),
		},
	}

	result, err := engine.ExecuteFlowDefinition(flowInstance, map[string]any{"route": "a"}, nil)
	require.NoError(t, err)
	require.True(t, result.Success)

	assert.NotNil(t, nodeA.executedAt, "nodeA should execute")
	assert.Nil(t, nodeB.executedAt, "nodeB should be skipped")
	require.NotNil(t, nodeJ.executedAt, "nodeJ should execute once on the live A path")

	jResult := result.ExecutionResults["nodeJ"]
	require.NotNil(t, jResult)
	assert.Equal(t, "fromA", jResult.GetInputs()["nodeA.value"], "J should receive A's data")

	// nodeB skipped, J succeeded — no leftover unreachable nodes.
	requireSkipped(t, result.ExecutionResults["nodeB"])
}

// (b2) cross-arm diamond: J is reachable from the taken arm (A) but templates an
// output from the ROUTED-AWAY arm (B). J must be SKIPPED with a
// dependency_skipped reason — NOT hard-fail validateInputs and error the flow.
func TestBranchRouting_DiamondJoinReferencesRoutedAwayArm(t *testing.T) {
	branch := branchNodeForTest(t, "router", "nodeA", "nodeB", "")
	nodeA := newDataContractMockNode("nodeA", nil, []string{"value"})
	nodeA.outputs = map[string]any{"value": "fromA"}
	nodeB := newDataContractMockNode("nodeB", nil, []string{"value"})
	nodeB.outputs = map[string]any{"value": "fromB"}
	// J reads B's output, but B is on the routed-away arm and gets skipped.
	nodeJ := newDataContractMockNode("nodeJ", []string{"nodeB.value"}, []string{"joined"})
	nodeJ.outputs = map[string]any{"joined": "ok"}

	flowInstance := flow.Flow{
		Name:          "branch-diamond-crossarm",
		Version:       "1.0",
		InitialInputs: map[string]any{"route": "a"},
		Nodes:         []node.AnyNode{branch, nodeA, nodeB, nodeJ},
		Edges: []edge.Edge{
			successEdge("router", "nodeA"),
			successEdge("router", "nodeB"),
			successEdge("nodeA", "nodeJ"),
			successEdge("nodeB", "nodeJ"),
		},
	}

	result, err := engine.ExecuteFlowDefinition(flowInstance, map[string]any{"route": "a"}, nil)
	// Graceful skip, not a NODE_INPUT_VALIDATION_FAILED hard failure.
	require.NoError(t, err)
	require.True(t, result.Success)

	assert.NotNil(t, nodeA.executedAt, "nodeA should execute")
	assert.Nil(t, nodeB.executedAt, "nodeB should be skipped")
	assert.Nil(t, nodeJ.executedAt, "nodeJ must be skipped, not run into validateInputs")

	requireSkipped(t, result.ExecutionResults["nodeB"])
	jResult := result.ExecutionResults["nodeJ"]
	requireSkipped(t, jResult)
	skippedJ, ok := skippedBaseOf(jResult)
	require.True(t, ok)
	require.NotNil(t, skippedJ.SkipReason)
	assert.Equal(t, "dependency_skipped", *skippedJ.SkipReason,
		"J skip reason should name the skipped upstream dependency")
}

// (c) nested branch inside the taken path.
func TestBranchRouting_NestedBranch(t *testing.T) {
	outer := branchNodeForTest(t, "outer", "inner", "skipped", "")
	inner := innerBranchForTest(t, "inner", "leafX", "leafY")
	leafX := newDataContractMockNode("leafX", nil, nil)
	leafY := newDataContractMockNode("leafY", nil, nil)
	skipped := newDataContractMockNode("skipped", nil, nil)

	flowInstance := flow.Flow{
		Name:          "branch-nested",
		Version:       "1.0",
		InitialInputs: map[string]any{"route": "a", "inner": "x"},
		Nodes:         []node.AnyNode{outer, inner, leafX, leafY, skipped},
		Edges: []edge.Edge{
			successEdge("outer", "inner"),
			successEdge("outer", "skipped"),
			successEdge("inner", "leafX"),
			successEdge("inner", "leafY"),
		},
	}

	result, err := engine.ExecuteFlowDefinition(
		flowInstance, map[string]any{"route": "a", "inner": "x"}, nil,
	)
	require.NoError(t, err)
	require.True(t, result.Success)

	assert.NotNil(t, leafX.executedAt, "leafX should execute (inner routed to x)")
	assert.Nil(t, leafY.executedAt, "leafY should be skipped")
	assert.Nil(t, skipped.executedAt, "outer's other branch should be skipped")
	requireSkipped(t, result.ExecutionResults["leafY"])
	requireSkipped(t, result.ExecutionResults["skipped"])
}

// (d) branch with default when no case matches.
func TestBranchRouting_DefaultPath(t *testing.T) {
	branch := branchNodeForTest(t, "router", "nodeA", "nodeB", "nodeC")
	nodeA := newDataContractMockNode("nodeA", nil, nil)
	nodeB := newDataContractMockNode("nodeB", nil, nil)
	nodeC := newDataContractMockNode("nodeC", nil, nil)

	flowInstance := flow.Flow{
		Name:          "branch-default",
		Version:       "1.0",
		InitialInputs: map[string]any{"route": "zzz"},
		Nodes:         []node.AnyNode{branch, nodeA, nodeB, nodeC},
		Edges: []edge.Edge{
			successEdge("router", "nodeA"),
			successEdge("router", "nodeB"),
			successEdge("router", "nodeC"),
		},
	}

	result, err := engine.ExecuteFlowDefinition(flowInstance, map[string]any{"route": "zzz"}, nil)
	require.NoError(t, err)
	require.True(t, result.Success)

	assert.NotNil(t, nodeC.executedAt, "default nodeC should execute")
	assert.Nil(t, nodeA.executedAt, "nodeA should be skipped")
	assert.Nil(t, nodeB.executedAt, "nodeB should be skipped")
	requireSkipped(t, result.ExecutionResults["nodeA"])
	requireSkipped(t, result.ExecutionResults["nodeB"])
}

// branch -> A, branch -> B with no join, take A: B subtree all skipped.
func TestBranchRouting_TreeShapedSkipsSubtree(t *testing.T) {
	branch := branchNodeForTest(t, "router", "nodeA", "nodeB", "")
	nodeA := newDataContractMockNode("nodeA", nil, nil)
	nodeB := newDataContractMockNode("nodeB", nil, nil)
	// nodeB1 is downstream of the skipped nodeB; the skip must cascade.
	nodeB1 := newDataContractMockNode("nodeB1", nil, nil)

	flowInstance := flow.Flow{
		Name:          "branch-tree",
		Version:       "1.0",
		InitialInputs: map[string]any{"route": "a"},
		Nodes:         []node.AnyNode{branch, nodeA, nodeB, nodeB1},
		Edges: []edge.Edge{
			successEdge("router", "nodeA"),
			successEdge("router", "nodeB"),
			successEdge("nodeB", "nodeB1"),
		},
	}

	result, err := engine.ExecuteFlowDefinition(flowInstance, map[string]any{"route": "a"}, nil)
	require.NoError(t, err)
	require.True(t, result.Success)

	assert.NotNil(t, nodeA.executedAt)
	assert.Nil(t, nodeB.executedAt)
	assert.Nil(t, nodeB1.executedAt, "cascade: nodeB1 below skipped nodeB should also skip")
	requireSkipped(t, result.ExecutionResults["nodeB"])
	requireSkipped(t, result.ExecutionResults["nodeB1"])
}

// innerBranchForTest builds a branch routing on initial input {{inner}}: "x" ->
// targetX, else targetY (default).
func innerBranchForTest(t *testing.T, id, targetX, targetY string) node.AnyNode {
	t.Helper()
	raw := `{
      "id": "` + id + `",
      "type": "branch",
      "data": {
        "target": "{{inner}}",
        "cases": [
          {
            "when": {
              "extractor_type": "body",
              "extractor_data": {},
              "operator_type": "equals",
              "operator_data": {"value": "x"}
            },
            "target": "` + targetX + `"
          }
        ],
        "default": "` + targetY + `"
      }
    }`
	parsed, err := node.UnmarshalNode([]byte(raw))
	require.NoError(t, err)
	return parsed
}

func requireSkipped(t *testing.T, result spi.AnyResult) {
	t.Helper()
	require.NotNil(t, result)
	// All skipped results carry the NODE_SKIPPED error code via BaseExecutionResult.
	skipped, ok := skippedBaseOf(result)
	require.True(t, ok, "result should expose a skip error code")
	require.NotNil(t, skipped.ErrorCode)
	assert.Equal(t, "NODE_SKIPPED", *skipped.ErrorCode)
	require.NotNil(t, skipped.SkipReason)
}

func skippedBaseOf(result spi.AnyResult) (*spi.BaseExecutionResult, bool) {
	switch r := result.(type) {
	case *spi.BaseExecutionResult:
		return r, true
	case *node.RequestExecutionResult:
		return &r.BaseExecutionResult, true
	case *node.BranchExecutionResult:
		return &r.BaseExecutionResult, true
	case *node.DelayExecutionResult:
		return &r.BaseExecutionResult, true
	case *node.ModuleExecutionResult:
		return &r.BaseExecutionResult, true
	default:
		return nil, false
	}
}
