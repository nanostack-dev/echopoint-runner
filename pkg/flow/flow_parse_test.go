package flow_test

import (
	"os"
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/edge"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/flow"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleParseFromJson(t *testing.T) {
	file, err := os.ReadFile("test.json")
	require.NoError(t, err, "should read test.json file")
	flow, err := flow.ParseFromJSON(file)
	require.NoError(t, err, "should parse from json")
	require.NotNil(t, flow, "flow should not be nil")

	// Validate flow metadata
	assert.Equal(t, "User API Test", flow.Name, "flow name should match")
	assert.Equal(
		t, "Test user endpoints with branching", flow.Description, "flow description should match",
	)
	assert.Equal(t, "1.0", flow.Version, "flow version should match")
	require.Len(t, flow.Nodes, 3, "should have 3 nodes")
	require.Len(t, flow.Edges, 2, "should have 2 edges")

	t.Run(
		"RequestNode1", func(t *testing.T) {
			reqNode, ok := node.AsRequestNode(flow.Nodes[0])
			require.True(t, ok, "first node should be a RequestNode")

			assert.Equal(t, "req-1", reqNode.GetID(), "request node 1 id should match")
			assert.Equal(
				t, node.TypeRequest, reqNode.GetType(), "request node 1 type should be request",
			)

			data := reqNode.GetData()
			assert.Equal(t, "POST", data.Method, "method should be POST")
			assert.Equal(t, "https://api.example.com/users", data.URL, "url should match")

			// Validate headers
			assert.Equal(
				t, "application/json", data.Headers["Content-Type"],
				"Content-Type header should match",
			)

			// Validate body
			body, ok := data.Body.(map[string]interface{})
			require.True(t, ok, "body should be a map")
			assert.Equal(t, "John Doe", body["name"], "body name should match")
			assert.Equal(t, "john@example.com", body["email"], "body email should match")

			assert.Equal(t, 30000, data.Timeout, "timeout should be 30000")

			require.Len(t, data.Assertions, 2, "should have 2 assertions")
		},
	)

	t.Run(
		"RequestNode2_Success", func(t *testing.T) {
			reqNode, ok := node.AsRequestNode(flow.Nodes[1])
			require.True(t, ok, "second node should be a RequestNode")

			assert.Equal(t, "req-success", reqNode.GetID(), "success node id should match")
			assert.Equal(
				t, node.TypeRequest, reqNode.GetType(), "success node type should be request",
			)

			data := reqNode.GetData()
			assert.Equal(t, "GET", data.Method, "method should be GET")
			assert.Equal(t, "https://api.example.com/users", data.URL, "url should match")

			require.Len(t, data.Assertions, 1, "should have 1 assertion")
		},
	)

	t.Run(
		"RequestNode3_Failure", func(t *testing.T) {
			reqNode, ok := node.AsRequestNode(flow.Nodes[2])
			require.True(t, ok, "third node should be a RequestNode")

			assert.Equal(t, "req-error", reqNode.GetID(), "error node id should match")
			assert.Equal(
				t, node.TypeRequest, reqNode.GetType(), "error node type should be request",
			)

			data := reqNode.GetData()
			assert.Equal(t, "POST", data.Method, "method should be POST")
			assert.Equal(t, "https://api.example.com/error-log", data.URL, "url should match")

			body, ok := data.Body.(map[string]interface{})
			require.True(t, ok, "body should be a map")
			assert.Equal(t, "User creation failed", body["error"], "error message should match")

			assert.Empty(t, data.Assertions, "should have 0 assertions")
		},
	)

	t.Run(
		"Edges", func(t *testing.T) {
			edge1 := flow.Edges[0]
			assert.Equal(t, "e-success", edge1.ID, "edge 1 id should match")
			assert.Equal(t, "req-1", edge1.Source, "edge 1 source should be req-1")
			assert.Equal(t, "req-success", edge1.Target, "edge 1 target should be req-success")
			assert.Equal(t, edge.TypeSuccess, edge1.Type, "edge 1 type should be success")

			edge2 := flow.Edges[1]
			assert.Equal(t, "e-failure", edge2.ID, "edge 2 id should match")
			assert.Equal(t, "req-1", edge2.Source, "edge 2 source should be req-1")
			assert.Equal(t, "req-error", edge2.Target, "edge 2 target should be req-error")
			assert.Equal(t, edge.TypeFailure, edge2.Type, "edge 2 type should be failure")
		},
	)
}

func TestParseFromJSON_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{"invalid": "json"`)
	flowResult, err := flow.ParseFromJSON(invalidJSON)
	require.Error(t, err, "should return error for invalid JSON")
	assert.Nil(t, flowResult, "flow should be nil on error")
}

func TestParseFromJSON_EmptyNodes(t *testing.T) {
	emptyNodesJSON := []byte(`{
		"version": "1.0",
		"name": "Empty Flow",
		"description": "Flow with no nodes",
		"nodes": [],
		"edges": []
	}`)
	flowResult, err := flow.ParseFromJSON(emptyNodesJSON)
	require.NoError(t, err, "should parse successfully")
	assert.Empty(t, flowResult.Nodes, "should have 0 nodes")
	assert.Empty(t, flowResult.Edges, "should have 0 edges")
}
