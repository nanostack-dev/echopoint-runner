package engine_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/internal/logger"
	"github.com/nanostack-dev/echopoint-runner/pkg/edge"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticModuleResolver map[string]node.ResolvedModuleFlow

func (r staticModuleResolver) ResolveFlow(flowID string) (node.ResolvedModuleFlow, bool) {
	resolved, ok := r[flowID]
	return resolved, ok
}

func init() {
	// Enable debug logging with human-readable format for tests
	logger.SetDebugLogging()
}

// ========== LEGACY TESTS - Old Execute() signature ==========

// MockNode is a test node that tracks execution (for legacy tests).
type MockNode struct {
	id          string
	nodeType    node.Type
	runWhen     node.RunWhen
	executed    bool
	shouldPass  bool
	shouldError bool
}

func (n *MockNode) GetID() string {
	return n.id
}

func (n *MockNode) GetDisplayName() string {
	return n.id
}

func (n *MockNode) GetType() node.Type {
	return n.nodeType
}

func (n *MockNode) GetRunWhen() node.RunWhen {
	if n.runWhen == "" {
		return node.RunWhenOnSuccess
	}
	return n.runWhen
}

func (n *MockNode) InputSchema() []string {
	return []string{}
}

func (n *MockNode) OutputSchema() []string {
	return []string{}
}

func (n *MockNode) GetAssertions() []node.CompositeAssertion {
	return []node.CompositeAssertion{}
}

func (n *MockNode) GetOutputs() []node.Output {
	return []node.Output{}
}

func (n *MockNode) Execute(_ node.ExecutionContext) (node.AnyExecutionResult, error) {
	n.executed = true

	outputs := map[string]interface{}{}
	var err error

	if n.shouldError {
		err = errors.New("mock error")
		errMsg := err.Error()
		errCode := "MOCK_ERROR"
		return &node.BaseExecutionResult{
			NodeID:      n.id,
			DisplayName: n.id,
			NodeType:    n.nodeType,
			RunWhen:     n.GetRunWhen(),
			Outputs:     nil,
			Error:       err,
			ErrorMsg:    &errMsg,
			ErrorCode:   &errCode,
			ExecutedAt:  time.Now(),
		}, err
	}

	return &node.BaseExecutionResult{
		NodeID:      n.id,
		DisplayName: n.id,
		NodeType:    n.nodeType,
		RunWhen:     n.GetRunWhen(),
		Outputs:     outputs,
		ExecutedAt:  time.Now(),
	}, nil
}

// ========== DATA CONTRACT TESTS - New Execute() signature ==========

// DataContractMockNode implements the full data contract interface for testing.
type DataContractMockNode struct {
	id          string
	nodeType    node.Type
	runWhen     node.RunWhen
	inputDeps   []string
	outputKeys  []string
	outputs     map[string]interface{}
	shouldError bool
	executedAt  *time.Time
}

func (n *DataContractMockNode) GetID() string {
	return n.id
}

func (n *DataContractMockNode) GetDisplayName() string {
	return n.id
}

func (n *DataContractMockNode) GetType() node.Type {
	return n.nodeType
}

func (n *DataContractMockNode) GetRunWhen() node.RunWhen {
	if n.runWhen == "" {
		return node.RunWhenOnSuccess
	}
	return n.runWhen
}

func (n *DataContractMockNode) InputSchema() []string {
	return n.inputDeps
}

func (n *DataContractMockNode) OutputSchema() []string {
	return n.outputKeys
}

func (n *DataContractMockNode) Execute(ctx node.ExecutionContext) (node.AnyExecutionResult, error) {
	now := time.Now()
	n.executedAt = &now

	// Validate required inputs
	for _, dep := range n.inputDeps {
		if _, exists := ctx.Inputs[dep]; !exists {
			err := errors.New("missing required input: " + dep)
			errMsg := err.Error()
			errCode := "MISSING_INPUT"
			return &node.BaseExecutionResult{
				NodeID:      n.id,
				DisplayName: n.id,
				NodeType:    n.nodeType,
				RunWhen:     n.GetRunWhen(),
				Inputs:      ctx.Inputs,
				Outputs:     nil,
				Error:       err,
				ErrorMsg:    &errMsg,
				ErrorCode:   &errCode,
				ExecutedAt:  now,
			}, err
		}
	}

	if n.shouldError {
		err := errors.New("intentional error in " + n.id)
		errMsg := err.Error()
		errCode := "INTENTIONAL_ERROR"
		return &node.BaseExecutionResult{
			NodeID:     n.id,
			NodeType:   n.nodeType,
			RunWhen:    n.GetRunWhen(),
			Inputs:     ctx.Inputs,
			Outputs:    nil,
			Error:      err,
			ErrorMsg:   &errMsg,
			ErrorCode:  &errCode,
			ExecutedAt: now,
		}, err
	}

	return &node.BaseExecutionResult{
		NodeID:      n.id,
		DisplayName: n.id,
		NodeType:    n.nodeType,
		RunWhen:     n.GetRunWhen(),
		Inputs:      ctx.Inputs,
		Outputs:     n.outputs,
		ExecutedAt:  now,
	}, nil
}

func (n *DataContractMockNode) GetAssertions() []node.CompositeAssertion {
	return []node.CompositeAssertion{}
}

func (n *DataContractMockNode) GetOutputs() []node.Output {
	return []node.Output{}
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

type testObserver struct {
	flowStarts   []engine.FlowStartedEvent
	nodeStarts   []engine.NodeStartedEvent
	nodeFinishes []engine.NodeFinishedEvent
	flowFinishes []engine.FlowFinishedEvent
}

func (o *testObserver) FlowStarted(evt engine.FlowStartedEvent) {
	o.flowStarts = append(o.flowStarts, evt)
}

func (o *testObserver) NodeStarted(evt engine.NodeStartedEvent) {
	o.nodeStarts = append(o.nodeStarts, evt)
}

func (o *testObserver) NodeFinished(evt engine.NodeFinishedEvent) {
	o.nodeFinishes = append(o.nodeFinishes, evt)
}

func (o *testObserver) FlowFinished(evt engine.FlowFinishedEvent) {
	o.flowFinishes = append(o.flowFinishes, evt)
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

	observer := &testObserver{}
	var executionOrder []string
	flowEngine, err := engine.NewFlowEngine(
		flowInstance, &engine.Options{
			Observer: observer,
		},
	)
	require.NoError(t, err)
	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)
	for _, evt := range observer.nodeStarts {
		executionOrder = append(executionOrder, evt.NodeID)
	}
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

func TestFlowEngine_Execute_AlwaysNodeRunsAfterMainFailure(t *testing.T) {
	create := newDataContractMockNode("create", nil, []string{"resourceId"})
	create.outputs["resourceId"] = "res-123"
	fail := newDataContractMockNode("fail", []string{"create.resourceId"}, nil)
	fail.shouldError = true
	cleanup := newDataContractMockNode("cleanup", []string{"create.resourceId"}, nil)
	cleanup.runWhen = node.RunWhenAlways

	flowInstance := flow.Flow{
		Name:  "Always After Failure",
		Nodes: []node.AnyNode{create, fail, cleanup},
		Edges: []edge.Edge{
			{ID: "e1", Source: "create", Target: "fail", Type: edge.TypeSuccess},
			{ID: "e2", Source: "create", Target: "cleanup", Type: edge.TypeSuccess},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]interface{}{})
	require.Error(t, err)
	require.False(t, result.Success)
	assert.NotNil(t, cleanup.executedAt)
	assert.Contains(t, result.ExecutionResults, "cleanup")
	assert.Contains(t, result.FinalOutputs, "create.resourceId")
	assert.Contains(t, result.Error.Error(), "fail")
	cleanupResult := result.ExecutionResults["cleanup"]
	assert.NoError(t, cleanupResult.GetError())
}

func TestFlowEngine_Execute_AlwaysNodeSkippedWhenInputsMissing(t *testing.T) {
	fail := newDataContractMockNode("fail", nil, nil)
	fail.shouldError = true
	cleanup := newDataContractMockNode("cleanup", []string{"create.resourceId"}, nil)
	cleanup.runWhen = node.RunWhenAlways

	flowInstance := flow.Flow{
		Name:    "Always Skipped",
		Nodes:   []node.AnyNode{fail, cleanup},
		Edges:   []edge.Edge{},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]interface{}{})
	require.Error(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.ExecutionResults, "cleanup")
	skipped := result.ExecutionResults["cleanup"]
	require.NoError(t, skipped.GetError())
	requestResult, ok := skipped.(*node.RequestExecutionResult)
	require.True(t, ok)
	base := requestResult.BaseExecutionResult
	require.NotNil(t, base.SkipReason)
	assert.Equal(t, "missing_inputs", *base.SkipReason)
	assert.Equal(t, []string{"create.resourceId"}, base.MissingInputs)
	assert.Nil(t, cleanup.executedAt)
}

func TestFlowEngine_Execute_AlwaysCleanupChainContinuesAfterIntermediateSkip(t *testing.T) {
	login := newDataContractMockNode("step-login", []string{"email", "password"}, []string{"accessToken"})
	login.outputs["accessToken"] = "token-123"

	createProduct := newDataContractMockNode(
		"step-create-product",
		[]string{"step-login.accessToken"},
		[]string{"productId"},
	)
	createProduct.outputs["productId"] = "prod-123"

	createAPIKey := newDataContractMockNode(
		"step-create-api-key",
		[]string{"step-login.accessToken", "step-create-product.productId"},
		[]string{"apiKeyId"},
	)
	createAPIKey.shouldError = true

	deleteAPIKey := newDataContractMockNode(
		"step-delete-api-key",
		[]string{
			"step-login.accessToken",
			"step-create-product.productId",
			"step-create-api-key.apiKeyId",
		},
		nil,
	)
	deleteAPIKey.runWhen = node.RunWhenAlways

	deleteProduct := newDataContractMockNode(
		"step-delete-product",
		[]string{"step-login.accessToken", "step-create-product.productId"},
		nil,
	)
	deleteProduct.runWhen = node.RunWhenAlways

	flowInstance := flow.Flow{
		Name: "Platform API Key CRUD Cleanup",
		Nodes: []node.AnyNode{
			login,
			createProduct,
			createAPIKey,
			deleteAPIKey,
			deleteProduct,
		},
		Edges: []edge.Edge{
			{ID: "e1", Source: "step-login", Target: "step-create-product", Type: edge.TypeSuccess},
			{ID: "e2", Source: "step-create-product", Target: "step-create-api-key", Type: edge.TypeSuccess},
			{ID: "e3", Source: "step-create-api-key", Target: "step-delete-api-key", Type: edge.TypeSuccess},
			{ID: "e4", Source: "step-delete-api-key", Target: "step-delete-product", Type: edge.TypeSuccess},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]interface{}{
		"email":    "alexis@nanostack.dev",
		"password": "secret",
	})
	require.Error(t, err)
	require.False(t, result.Success)

	assert.NotNil(t, login.executedAt)
	assert.NotNil(t, createProduct.executedAt)
	assert.NotNil(t, createAPIKey.executedAt)
	assert.Nil(t, deleteAPIKey.executedAt)
	assert.NotNil(t, deleteProduct.executedAt)

	require.Contains(t, result.ExecutionResults, "step-delete-api-key")
	require.Contains(t, result.ExecutionResults, "step-delete-product")

	deleteAPIKeyResult, ok := result.ExecutionResults["step-delete-api-key"].(*node.RequestExecutionResult)
	require.True(t, ok)
	require.NotNil(t, deleteAPIKeyResult.SkipReason)
	assert.Equal(t, "missing_inputs", *deleteAPIKeyResult.SkipReason)
	assert.Equal(t, []string{"step-create-api-key.apiKeyId"}, deleteAPIKeyResult.MissingInputs)

	deleteProductResult := result.ExecutionResults["step-delete-product"]
	require.NoError(t, deleteProductResult.GetError())
	assert.Equal(t, "prod-123", deleteProductResult.GetInputs()["step-create-product.productId"])
	assert.Equal(t, "token-123", deleteProductResult.GetInputs()["step-login.accessToken"])
}

func TestFlowEngine_Execute_AlwaysCleanupJoinRunsAfterUpstreamCleanupIsSkipped(t *testing.T) {
	createProduct := newDataContractMockNode("step-create-product", nil, []string{"productId"})
	createProduct.outputs["productId"] = "prod-123"

	// This branch fails first and aborts the main phase before search-roles runs.
	failMidFlow := newDataContractMockNode("step-fail-mid-flow", []string{"step-create-product.productId"}, nil)
	failMidFlow.shouldError = true

	// This setup branch would normally unlock cleanup, but it never gets to finish
	// the main phase once fail-mid-flow errors.
	prepareRoleSearch := newDataContractMockNode(
		"step-prepare-role-search",
		[]string{"step-create-product.productId"},
		nil,
	)
	searchRoles := newDataContractMockNode("step-search-roles", nil, nil)

	deleteRole := newDataContractMockNode(
		"step-delete-role",
		[]string{"step-create-product.productId"},
		nil,
	)
	deleteRole.runWhen = node.RunWhenAlways

	deleteProduct := newDataContractMockNode(
		"step-delete-product",
		[]string{"step-create-product.productId"},
		nil,
	)
	deleteProduct.runWhen = node.RunWhenAlways

	flowInstance := flow.Flow{
		Name: "Cleanup Join After Skipped Upstream Cleanup",
		Nodes: []node.AnyNode{
			createProduct,
			failMidFlow,
			prepareRoleSearch,
			searchRoles,
			deleteRole,
			deleteProduct,
		},
		Edges: []edge.Edge{
			{ID: "e1", Source: "step-create-product", Target: "step-fail-mid-flow", Type: edge.TypeSuccess},
			{ID: "e2", Source: "step-create-product", Target: "step-prepare-role-search", Type: edge.TypeSuccess},
			{ID: "e3", Source: "step-prepare-role-search", Target: "step-search-roles", Type: edge.TypeSuccess},
			{ID: "e4", Source: "step-search-roles", Target: "step-delete-role", Type: edge.TypeSuccess},
			{ID: "e5", Source: "step-delete-role", Target: "step-delete-product", Type: edge.TypeSuccess},
		},
		Version: "1.0",
	}

	flowEngine, err := engine.NewFlowEngine(flowInstance, &engine.Options{})
	require.NoError(t, err)

	result, err := flowEngine.Execute(map[string]interface{}{})
	require.Error(t, err)
	require.False(t, result.Success)

	assert.NotNil(t, createProduct.executedAt)
	assert.NotNil(t, failMidFlow.executedAt)
	assert.NotNil(t, prepareRoleSearch.executedAt)
	assert.Nil(t, searchRoles.executedAt)
	assert.Nil(t, deleteRole.executedAt)
	assert.NotNil(t, deleteProduct.executedAt)

	require.Contains(t, result.ExecutionResults, "step-delete-role")
	require.Contains(t, result.ExecutionResults, "step-delete-product")

	deleteRoleResult, ok := result.ExecutionResults["step-delete-role"].(*node.RequestExecutionResult)
	require.True(t, ok)
	require.NotNil(t, deleteRoleResult.SkipReason)
	assert.Equal(t, "not_reachable_after_main_phase", *deleteRoleResult.SkipReason)

	deleteProductResult := result.ExecutionResults["step-delete-product"]
	require.NoError(t, deleteProductResult.GetError())
	assert.Equal(t, "prod-123", deleteProductResult.GetInputs()["step-create-product.productId"])
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

func TestFlowEngine_Execute_WithObserver(t *testing.T) {
	node1 := &MockNode{id: "node1", nodeType: node.TypeRequest, shouldPass: true}
	node2 := &MockNode{id: "node2", nodeType: node.TypeRequest, shouldPass: true}

	observer := &testObserver{}

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
			Observer: observer,
		},
	)
	require.NoError(t, err)

	result, err := flowEngine.Execute(make(map[string]interface{}))

	require.NoError(t, err)
	require.True(t, result.Success)
	var beforeCalls []string
	var afterCalls []string
	for _, evt := range observer.nodeStarts {
		beforeCalls = append(beforeCalls, evt.NodeID)
	}
	for _, evt := range observer.nodeFinishes {
		afterCalls = append(afterCalls, evt.NodeID)
	}
	assert.Equal(
		t, []string{"node1", "node2"}, beforeCalls,
		"node started should be observed for each node",
	)
	assert.Equal(
		t, []string{"node1", "node2"}, afterCalls, "node finished should be observed for each node",
	)
	require.Len(t, observer.flowStarts, 1)
	require.Len(t, observer.flowFinishes, 1)
	assert.Equal(t, flowInstance.Name, observer.flowStarts[0].FlowName)
	assert.Equal(t, flowInstance.Name, observer.flowFinishes[0].FlowName)
	assert.True(t, observer.flowFinishes[0].Result.Success)
}

// ========== DATA CONTRACT TESTS ==========

// TestDataContract_SimpleDataPassing tests basic multi-node data passing.
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
	frame2 := result.ExecutionResults["fetch-user"]
	assert.Equal(t, "user-123", frame2.GetInputs()["create-user.userId"])
}

// TestDataContract_MissingInput tests error handling for missing inputs.
func TestDataContract_MissingInput(t *testing.T) {
	dataContractMockNode := newDataContractMockNode(
		"dataContractMockNode", []string{"required"}, []string{"output"},
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
	assert.Contains(
		t, err.Error(),
		"node dataContractMockNode: output 'required' not found in source node ''",
	)
}

// TestDataContract_ExecutionResults tests complete execution tracing.
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
	frame1 := result.ExecutionResults["step1"]
	assert.NotNil(t, frame1.GetExecutedAt())
	require.NoError(t, frame1.GetError())
	assert.Equal(t, map[string]interface{}{"output": "value1"}, frame1.GetOutputs())

	frame2 := result.ExecutionResults["step2"]
	assert.Equal(t, "value1", frame2.GetInputs()["step1.output"])
	assert.Equal(t, map[string]interface{}{"output": "value2"}, frame2.GetOutputs())
}

func TestFlowEngine_Execute_ModuleNodeExportsNestedOutputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/charge":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"chargeId":"ch_123","status":"authorized"}`))
		case "/notify":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"notificationId":"notif_456"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	parentJSON := []byte(`{
		"name": "Parent Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "charge-customer",
				"display_name": "Charge Customer",
				"type": "module",
				"data": {
					"flow_id": "flow-charge",
					"input_bindings": {
						"customerId": "{{customerId}}",
						"currency": "{{currency}}"
					},
					"output_bindings": {
						"chargeId": "create-charge.chargeId",
						"status": "create-charge.status"
					}
				}
			},
			{
				"id": "notify-customer",
				"display_name": "Notify Customer",
				"type": "module",
				"data": {
					"flow_id": "flow-notify",
					"input_bindings": {
						"chargeId": "{{charge-customer.chargeId}}",
						"status": "{{charge-customer.status}}"
					},
					"output_bindings": {
						"notificationId": "send-receipt.notificationId"
					}
				}
			}
		],
		"edges": [
			{"id": "e1", "source": "charge-customer", "target": "notify-customer", "type": "success"}
		]
	}`)

	childChargeJSON := []byte(`{
		"name": "Charge Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "create-charge",
				"display_name": "Create Charge",
				"type": "request",
				"data": {
					"method": "POST",
					"url": "` + server.URL + `/charge",
					"body": {
						"customerId": "{{customerId}}",
						"currency": "{{currency}}"
					},
					"timeout": 1000
				},
				"outputs": [
					{"name": "chargeId", "extractor": {"type": "jsonPath", "path": "$.chargeId"}},
					{"name": "status", "extractor": {"type": "jsonPath", "path": "$.status"}}
				]
			}
		],
		"edges": [],
		"initialInputs": {
			"customerId": "cust-default",
			"currency": "usd"
		}
	}`)

	childNotifyJSON := []byte(`{
		"name": "Notify Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "send-receipt",
				"display_name": "Send Receipt",
				"type": "request",
				"data": {
					"method": "POST",
					"url": "` + server.URL + `/notify",
					"body": {
						"chargeId": "{{chargeId}}",
						"status": "{{status}}"
					},
					"timeout": 1000
				},
				"outputs": [
					{"name": "notificationId", "extractor": {"type": "jsonPath", "path": "$.notificationId"}}
				]
			}
		],
		"edges": [],
		"initialInputs": {
			"chargeId": "fallback",
			"status": "pending"
		}
	}`)

	parentFlow, err := flow.ParseFromJSONWithOptions(parentJSON, flow.ParseOptions{
		AllowedInitialInputKeys: []string{"customerId", "currency"},
	})
	require.NoError(t, err)

	resolver := staticModuleResolver{
		"flow-charge": {
			FlowDefinition: childChargeJSON,
			Environment: map[string]string{
				"currency": "eur",
			},
		},
		"flow-notify": {
			FlowDefinition: childNotifyJSON,
			Environment:    map[string]string{},
		},
	}

	result, err := engine.ExecuteFlowDefinition(*parentFlow, map[string]interface{}{
		"customerId": "cust-123",
		"currency":   "gbp",
	}, &engine.ExecuteOptions{
		ModuleResolver: resolver,
	})
	require.NoError(t, err)
	require.True(t, result.Success)

	chargeResult := node.MustAsModuleExecutionResult(result.ExecutionResults["charge-customer"])
	assert.Equal(t, "flow-charge", chargeResult.FlowID)
	assert.Equal(t, "cust-123", chargeResult.GetInputs()["customerId"])
	assert.Equal(t, "gbp", chargeResult.GetInputs()["currency"])
	assert.Contains(t, chargeResult.ChildFinalOutputs, "create-charge.chargeId")

	notifyResult := node.MustAsModuleExecutionResult(result.ExecutionResults["notify-customer"])
	assert.Equal(t, chargeResult.GetOutputs()["chargeId"], notifyResult.GetInputs()["charge-customer.chargeId"])
	assert.Equal(t, chargeResult.GetOutputs()["status"], notifyResult.GetInputs()["charge-customer.status"])

	assert.Contains(t, result.FinalOutputs, "charge-customer.chargeId")
	assert.Contains(t, result.FinalOutputs, "charge-customer.status")
	assert.Contains(t, result.FinalOutputs, "notify-customer.notificationId")
}

func TestFlowEngine_Execute_ModuleNodeFailsWhenChildOutputBindingMissing(t *testing.T) {
	parentJSON := []byte(`{
		"name": "Parent Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "module-step",
				"display_name": "Module Step",
				"type": "module",
				"data": {
					"flow_id": "flow-child",
					"output_bindings": {
						"missing": "child-step.missing"
					}
				}
			}
		],
		"edges": []
	}`)

	childJSON := []byte(`{
		"name": "Child Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "child-step",
				"display_name": "Child Step",
				"type": "delay",
				"data": {"duration": 1}
			}
		],
		"edges": []
	}`)

	parentFlow, err := flow.ParseFromJSON(parentJSON)
	require.NoError(t, err)

	resolver := staticModuleResolver{
		"flow-child": {
			FlowDefinition: childJSON,
		},
	}

	result, err := engine.ExecuteFlowDefinition(*parentFlow, map[string]interface{}{}, &engine.ExecuteOptions{
		ModuleResolver: resolver,
	})
	require.Error(t, err)
	require.False(t, result.Success)
	assert.Contains(t, err.Error(), "module output \"missing\" references unavailable child output")

	moduleResult := node.MustAsModuleExecutionResult(result.ExecutionResults["module-step"])
	require.Error(t, moduleResult.GetError())
	assert.Equal(t, "flow-child", moduleResult.FlowID)
}

func TestFlowEngine_Execute_ModuleNodeTrimsFlowID(t *testing.T) {
	parentJSON := []byte(`{
		"name": "Parent Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "module-step",
				"display_name": "Module Step",
				"type": "module",
				"data": {
					"flow_id": "  flow-child  "
				}
			}
		],
		"edges": []
	}`)

	childJSON := []byte(`{
		"name": "Child Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "child-step",
				"display_name": "Child Step",
				"type": "delay",
				"data": {"duration": 1}
			}
		],
		"edges": []
	}`)

	parentFlow, err := flow.ParseFromJSON(parentJSON)
	require.NoError(t, err)

	resolver := staticModuleResolver{
		"flow-child": {
			FlowDefinition: childJSON,
		},
	}

	result, err := engine.ExecuteFlowDefinition(*parentFlow, map[string]interface{}{}, &engine.ExecuteOptions{
		ModuleResolver: resolver,
	})
	require.NoError(t, err)
	require.True(t, result.Success)

	moduleResult := node.MustAsModuleExecutionResult(result.ExecutionResults["module-step"])
	assert.Equal(t, "flow-child", moduleResult.FlowID)
	assert.Empty(t, moduleResult.GetOutputs())
	assert.Empty(t, moduleResult.ChildFinalOutputs)
}

func TestFlowExecutionResultToPayload_IncludesModuleExecutionResult(t *testing.T) {
	result := &node.FlowExecutionResult{
		ExecutionResults: map[string]node.AnyExecutionResult{
			"charge-customer": &node.ModuleExecutionResult{
				BaseExecutionResult: node.BaseExecutionResult{
					NodeID:      "charge-customer",
					DisplayName: "Charge Customer",
					NodeType:    node.TypeModule,
					Inputs: map[string]interface{}{
						"customerId": "cust-123",
					},
					Outputs: map[string]interface{}{
						"chargeId": "ch_123",
					},
					ExecutedAt: time.Now(),
				},
				FlowID: "flow-charge",
				ChildFinalOutputs: map[string]interface{}{
					"create-charge.chargeId": "ch_123",
				},
				DurationMs: 42,
			},
		},
		FinalOutputs: map[string]interface{}{
			"charge-customer.chargeId": "ch_123",
		},
		Success:    true,
		DurationMS: 42,
	}

	payload, err := controlplane.FlowExecutionResultToPayload(result)
	require.NoError(t, err)
	require.Contains(t, payload, "execution_results")

	executionResults, ok := payload["execution_results"].(map[string]interface{})
	require.True(t, ok)
	modulePayload, ok := executionResults["charge-customer"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, string(node.TypeModule), modulePayload["node_type"])
	assert.Equal(t, "flow-charge", modulePayload["flow_id"])
	assert.Contains(t, modulePayload, "child_final_outputs")

	_, marshalErr := json.Marshal(payload)
	require.NoError(t, marshalErr)
}

func TestFlowEngine_Execute_ModuleNodeRejectsDirectCycle(t *testing.T) {
	parentJSON := []byte(`{
		"name": "Parent Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "module-step",
				"display_name": "Module Step",
				"type": "module",
				"data": {
					"flow_id": "flow-self"
				}
			}
		],
		"edges": []
	}`)

	childJSON := []byte(`{
		"name": "Self Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "call-self",
				"display_name": "Call Self",
				"type": "module",
				"data": {
					"flow_id": "flow-self"
				}
			}
		],
		"edges": []
	}`)

	parentFlow, err := flow.ParseFromJSON(parentJSON)
	require.NoError(t, err)

	resolver := staticModuleResolver{
		"flow-self": {
			FlowDefinition: childJSON,
		},
	}

	result, err := engine.ExecuteFlowDefinition(*parentFlow, map[string]interface{}{}, &engine.ExecuteOptions{
		ModuleResolver: resolver,
	})
	require.Error(t, err)
	require.False(t, result.Success)
	assert.Contains(t, err.Error(), "module cycle detected: flow-self -> flow-self")
	assert.Equal(t, "FLOW_VALIDATION_FAILED", *result.ErrorCode)
	assert.Empty(t, result.ExecutionResults)
	assert.Empty(t, result.FinalOutputs)
}

func TestFlowEngine_Execute_ModuleNodeRejectsIndirectCycle(t *testing.T) {
	parentJSON := []byte(`{
		"name": "Parent Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "start-module-a",
				"display_name": "Start Module A",
				"type": "module",
				"data": {
					"flow_id": "flow-a"
				}
			}
		],
		"edges": []
	}`)

	flowAJSON := []byte(`{
		"name": "Flow A",
		"version": "1.0",
		"nodes": [
			{
				"id": "call-b",
				"display_name": "Call B",
				"type": "module",
				"data": {
					"flow_id": "flow-b"
				}
			}
		],
		"edges": []
	}`)

	flowBJSON := []byte(`{
		"name": "Flow B",
		"version": "1.0",
		"nodes": [
			{
				"id": "call-a",
				"display_name": "Call A",
				"type": "module",
				"data": {
					"flow_id": "flow-a"
				}
			}
		],
		"edges": []
	}`)

	parentFlow, err := flow.ParseFromJSON(parentJSON)
	require.NoError(t, err)

	resolver := staticModuleResolver{
		"flow-a": {FlowDefinition: flowAJSON},
		"flow-b": {FlowDefinition: flowBJSON},
	}

	result, err := engine.ExecuteFlowDefinition(*parentFlow, map[string]interface{}{}, &engine.ExecuteOptions{
		ModuleResolver: resolver,
	})
	require.Error(t, err)
	require.False(t, result.Success)
	assert.Contains(t, err.Error(), "module cycle detected: flow-a -> flow-b -> flow-a")
	assert.Equal(t, "FLOW_VALIDATION_FAILED", *result.ErrorCode)
	assert.Empty(t, result.ExecutionResults)
	assert.Empty(t, result.FinalOutputs)
}

func TestFlowEngine_Execute_ModuleNodeRejectsIndirectCycleBeforeSideEffects(t *testing.T) {
	var (
		requestCount int
		mu           sync.Mutex
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	parentJSON := []byte(`{
		"name": "Parent Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "start-module-a",
				"display_name": "Start Module A",
				"type": "module",
				"data": {
					"flow_id": "flow-a"
				}
			}
		],
		"edges": []
	}`)

	flowAJSON := []byte(`{
		"name": "Flow A",
		"version": "1.0",
		"nodes": [
			{
				"id": "call-api-a",
				"display_name": "Call API A",
				"type": "request",
				"data": {
					"method": "POST",
					"url": "` + server.URL + `/a",
					"timeout": 1000
				}
			},
			{
				"id": "call-b",
				"display_name": "Call B",
				"type": "module",
				"data": {
					"flow_id": "flow-b"
				}
			}
		],
		"edges": [
			{"id": "e1", "source": "call-api-a", "target": "call-b", "type": "success"}
		]
	}`)

	flowBJSON := []byte(`{
		"name": "Flow B",
		"version": "1.0",
		"nodes": [
			{
				"id": "call-api-b",
				"display_name": "Call API B",
				"type": "request",
				"data": {
					"method": "POST",
					"url": "` + server.URL + `/b",
					"timeout": 1000
				}
			},
			{
				"id": "call-a",
				"display_name": "Call A",
				"type": "module",
				"data": {
					"flow_id": "flow-a"
				}
			}
		],
		"edges": [
			{"id": "e1", "source": "call-api-b", "target": "call-a", "type": "success"}
		]
	}`)

	parentFlow, err := flow.ParseFromJSON(parentJSON)
	require.NoError(t, err)

	resolver := staticModuleResolver{
		"flow-a": {FlowDefinition: flowAJSON},
		"flow-b": {FlowDefinition: flowBJSON},
	}

	result, err := engine.ExecuteFlowDefinition(*parentFlow, map[string]interface{}{}, &engine.ExecuteOptions{
		ModuleResolver: resolver,
	})
	require.Error(t, err)
	require.False(t, result.Success)
	assert.Contains(t, err.Error(), "module cycle detected: flow-a -> flow-b -> flow-a")
	assert.Equal(t, "FLOW_VALIDATION_FAILED", *result.ErrorCode)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 0, requestCount)
	assert.Empty(t, result.ExecutionResults)
	assert.Empty(t, result.FinalOutputs)
}
