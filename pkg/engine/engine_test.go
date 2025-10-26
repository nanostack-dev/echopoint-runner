package engine_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/edge"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/engine"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/flow"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== LEGACY TESTS - Old Execute() signature ==========

// MockNode is a test node that tracks execution (for legacy tests).
type MockNode struct {
	id          string
	nodeType    node.Type
	executed    bool
	shouldPass  bool
	shouldError bool
}

func (n *MockNode) GetID() string {
	return n.id
}

func (n *MockNode) GetType() node.Type {
	return n.nodeType
}

func (n *MockNode) InputSchema() []string {
	return []string{}
}

func (n *MockNode) OutputSchema() []string {
	return []string{}
}

func (n *MockNode) Execute(ctx node.ExecutionContext) (map[string]interface{}, error) {
	n.executed = true
	if n.shouldError {
		return nil, errors.New("mock error")
	}
	if !n.shouldPass {
		return map[string]interface{}{}, nil
	}
	return map[string]interface{}{}, nil
}

// ========== DATA CONTRACT TESTS - New Execute() signature ==========

// DataContractMockNode implements the full data contract interface for testing.
type DataContractMockNode struct {
	id          string
	nodeType    node.Type
	inputDeps   []string
	outputKeys  []string
	outputs     map[string]interface{}
	shouldError bool
	executedAt  *time.Time
}

func (n *DataContractMockNode) GetID() string {
	return n.id
}

func (n *DataContractMockNode) GetType() node.Type {
	return n.nodeType
}

func (n *DataContractMockNode) InputSchema() []string {
	return n.inputDeps
}

func (n *DataContractMockNode) OutputSchema() []string {
	return n.outputKeys
}

func (n *DataContractMockNode) Execute(ctx node.ExecutionContext) (map[string]interface{}, error) {
	now := time.Now()
	n.executedAt = &now

	// Validate required inputs
	for _, dep := range n.inputDeps {
		if _, exists := ctx.Inputs[dep]; !exists {
			return nil, errors.New("missing required input: " + dep)
		}
	}

	if n.shouldError {
		return nil, errors.New("intentional error in " + n.id)
	}

	return n.outputs, nil
}

func newDataContractMockNode(id string, inputDeps, outputKeys []string) *DataContractMockNode {
	return &DataContractMockNode{
		id:         id,
		nodeType:   node.TypeRequest,
		inputDeps:  inputDeps,
		outputKeys: outputKeys,
		outputs:    make(map[string]interface{}),
	}
}

func TestNewFlowEngine_Success(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:        "Test Flow",
		Description: "Test flow description",
		Nodes:       []node.AnyNode{node1, node2},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "node2", Type: "success"},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})

	require.NoError(t, err)
	assert.NotNil(t, flowEngine)
}

func TestNewFlowEngine_SourceNodeNotFound(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:  "Test Flow",
		Nodes: []node.AnyNode{node1},
		Edges: []edge.Edge{
			{ID: "e1", Source: "nonexistent", Target: "node1", Type: "success"},
		},
		Version: "1.0",
	}

	engine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})

	require.Error(t, err)
	assert.Nil(t, engine)
	assert.Contains(t, err.Error(), "source node nonexistent not found")
}

func TestNewFlowEngine_TargetNodeNotFound(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:  "Test Flow",
		Nodes: []node.AnyNode{node1},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "nonexistent", Type: "success"},
		},
		Version: "1.0",
	}

	engine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})

	require.Error(t, err)
	assert.Nil(t, engine)
	assert.Contains(t, err.Error(), "target node nonexistent not found")
}

func TestFlowEngine_Execute_LinearFlow(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldPass: true}
	node3 := &MockNode{id: "node3", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:  "Linear Flow",
		Nodes: []node.AnyNode{node1, node2, node3},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "node2", Type: "success"},
			{ID: "e2", Source: "node2", Target: "node3", Type: "success"},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)
	assert.True(t, node1.executed, "node1 should be executed")
	assert.True(t, node2.executed, "node2 should be executed")
	assert.True(t, node3.executed, "node3 should be executed")
}

func TestFlowEngine_Execute_ParallelFlow(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldPass: true}
	node3 := &MockNode{id: "node3", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:  "Parallel Flow",
		Nodes: []node.AnyNode{node1, node2, node3},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "node3", Type: "success"},
			{ID: "e2", Source: "node2", Target: "node3", Type: "success"},
		},
		Version: "1.0",
	}

	var executionOrder []string
	flowEngine, err := engine.NewFlowEngine(
		flowInstance, &engine.Options{
			BeforeExecution: func(n node.AnyNode) {
				executionOrder = append(executionOrder, n.GetID())
			},
		},
	)
	require.NoError(t, err)
	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)
	assert.True(t, node1.executed, "node1 should be executed")
	assert.True(t, node2.executed, "node2 should be executed")
	assert.True(t, node3.executed, "node3 should be executed after both node1 and node2")
	assert.ElementsMatch(
		t, []string{"node1", "node2"}, executionOrder[:2], "execution order should be node1, node2",
	)
}

func TestFlowEngine_Execute_BranchingFlow(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldPass: true}
	node3 := &MockNode{id: "node3", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:  "Branching Flow",
		Nodes: []node.AnyNode{node1, node2, node3},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "node2", Type: "success"},
			{ID: "e2", Source: "node1", Target: "node3", Type: "failure"},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)
	assert.True(t, node1.executed, "node1 should be executed")
	assert.True(t, node2.executed, "node2 should be executed")
	assert.True(t, node3.executed, "node3 should be executed")
}

func TestFlowEngine_Execute_NodeFailsWithError(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldError: true}
	node3 := &MockNode{id: "node3", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:  "Error Flow",
		Nodes: []node.AnyNode{node1, node2, node3},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "node2", Type: "success"},
			{ID: "e2", Source: "node2", Target: "node3", Type: "success"},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.Error(t, err)
	require.False(t, result.Success)
	assert.True(t, node1.executed, "node1 should be executed")
	assert.True(t, node2.executed, "node2 should be executed")
	assert.False(t, node3.executed, "node3 should not be executed due to error")
}

func TestFlowEngine_Execute_NodeDoesNotPass(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldPass: false}
	node3 := &MockNode{id: "node3", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:  "Non-passing Flow",
		Nodes: []node.AnyNode{node1, node2, node3},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "node2", Type: "success"},
			{ID: "e2", Source: "node2", Target: "node3", Type: "success"},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)
	assert.True(t, node1.executed, "node1 should be executed")
	assert.True(t, node2.executed, "node2 should be executed")
	assert.False(t, node3.executed, "node3 should not be executed when node2 doesn't pass")
}

func TestFlowEngine_Execute_NoNodes(t *testing.T) {
	flowInstance := flow.Flow{
		Name:    "Empty Flow",
		Nodes:   []node.AnyNode{},
		Edges:   []edge.Edge{},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.Error(t, err)
	require.False(t, result.Success)
	assert.Contains(t, err.Error(), "no nodes to execute")
}

func TestFlowEngine_Execute_CycleDetection(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldPass: true}

	flowInstance := flow.Flow{
		Name:  "Cyclic Flow",
		Nodes: []node.AnyNode{node1, node2},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "node2", Type: "success"},
			{ID: "e2", Source: "node2", Target: "node1", Type: "success"},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.Error(t, err)
	require.False(t, result.Success)
	assert.Contains(t, err.Error(), "cycle detected or unreachable nodes")
}

func TestFlowEngine_Execute_WithBeforeAndAfterCallbacks(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldPass: true}

	var beforeCalls []string
	var afterCalls []string

	flowInstance := flow.Flow{
		Name:  "Callback Flow",
		Nodes: []node.AnyNode{node1, node2},
		Edges: []edge.Edge{
			{ID: "e1", Source: "node1", Target: "node2", Type: "success"},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(
		flowInstance, &engine.Options{
			BeforeExecution: func(n node.AnyNode) {
				beforeCalls = append(beforeCalls, n.GetID())
			},
			AfterExecution: func(n node.AnyNode, frame node.ExecutionResult) {
				afterCalls = append(afterCalls, n.GetID())
			},
		},
	)
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)
	assert.Equal(
		t, []string{"node1", "node2"}, beforeCalls,
		"beforeExecution should be called for each node",
	)
	assert.Equal(
		t, []string{"node1", "node2"}, afterCalls, "afterExecution should be called for each node",
	)
}

// ========== DATA CONTRACT TESTS ==========

// TestDataContract_SimpleDataPassing tests basic multi-node data passing
func TestDataContract_SimpleDataPassing(t *testing.T) {
	node1 := newDataContractMockNode("create-user", []string{}, []string{"userId", "statusCode"})
	node1.outputs = map[string]interface{}{
		"userId":     "user-123",
		"statusCode": 201,
	}

	node2 := newDataContractMockNode(
		"fetch-user", []string{"create-user.userId"}, []string{"userData"},
	)
	node2.outputs = map[string]interface{}{
		"userData": map[string]interface{}{"name": "John", "email": "john@example.com"},
	}

	flowInstance := flow.Flow{
		Name:  "Data Passing Test",
		Nodes: []node.AnyNode{node1, node2},
		Edges: []edge.Edge{
			{ID: "e1", Source: "create-user", Target: "fetch-user", Type: "success"},
		},
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, nil)
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)

	// Verify node1 outputs
	assert.Equal(t, "user-123", result.FinalOutputs["create-user.userId"])
	assert.Equal(t, 201, result.FinalOutputs["create-user.statusCode"])

	// Verify node2 received data from node1
	frame2 := result.Frames["fetch-user"]
	assert.Equal(t, "user-123", frame2.Inputs["create-user.userId"])
}

// TestDataContract_MissingInput tests error handling for missing inputs
func TestDataContract_MissingInput(t *testing.T) {
	dataContractMockNode := newDataContractMockNode(
		"dataContractMockNode", []string{"initial.required"}, []string{"output"},
	)

	flowInstance := flow.Flow{
		Name:  "Missing Input Test",
		Nodes: []node.AnyNode{dataContractMockNode},
		Edges: []edge.Edge{},
		InitialInputs: map[string]interface{}{
			"provided": "value",
		},
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, nil)
	require.NoError(t, err)

	result, err := flowEngine.Execute(flowInstance.InitialInputs)

	require.Error(t, err)
	require.False(t, result.Success)
	assert.Contains(t, err.Error(), "missing required input")
}

// TestDataContract_ExecutionResults tests complete execution tracing
func TestDataContract_ExecutionResults(t *testing.T) {
	node1 := newDataContractMockNode("step1", []string{}, []string{"output"})
	node1.outputs = map[string]interface{}{"output": "value1"}

	node2 := newDataContractMockNode("step2", []string{"step1.output"}, []string{"output"})
	node2.outputs = map[string]interface{}{"output": "value2"}

	flowInstance := flow.Flow{
		Name:  "Frame Test",
		Nodes: []node.AnyNode{node1, node2},
		Edges: []edge.Edge{
			{ID: "e1", Source: "step1", Target: "step2", Type: "success"},
		},
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, nil)
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)

	// Verify frame structure
	frame1 := result.Frames["step1"]
	assert.NotNil(t, frame1.ExecutedAt)
	assert.Nil(t, frame1.Error)
	assert.Equal(t, map[string]interface{}{"output": "value1"}, frame1.Outputs)

	frame2 := result.Frames["step2"]
	assert.Equal(t, "value1", frame2.Inputs["step1.output"])
	assert.Equal(t, map[string]interface{}{"output": "value2"}, frame2.Outputs)
}
