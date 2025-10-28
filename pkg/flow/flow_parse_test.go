package flow_test

import (
	"os"
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/edge"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	httpextractors "github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors/http"
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

			// Validate assertions - they should have extractors now
			assertions := reqNode.GetAssertions()
			require.Len(t, assertions, 2, "should have 2 assertions")

			// First assertion should have StatusCode extractor
			firstAssertion := assertions[0]
			require.NotNil(t, firstAssertion.Extractor, "first assertion should have extractor")
			_, isStatusCode := firstAssertion.Extractor.(httpextractors.StatusCodeExtractor)
			assert.True(t, isStatusCode, "first assertion extractor should be StatusCodeExtractor")

			// Second assertion should have JSONPath extractor
			secondAssertion := assertions[1]
			require.NotNil(t, secondAssertion.Extractor, "second assertion should have extractor")
			jsonExt, ok := secondAssertion.Extractor.(extractors.JSONPathExtractor)
			assert.True(t, ok, "second assertion extractor should be JSONPathExtractor")
			assert.Equal(t, "$.user.id", jsonExt.Path, "JSONPath should match")

			// Validate outputs with extractors
			outputs := reqNode.GetOutputs()
			require.Len(t, outputs, 2, "should have 2 outputs")

			// First output: userId with JSONPath extractor
			assert.Equal(t, "userId", outputs[0].Name, "first output name should be userId")
			require.NotNil(t, outputs[0].Extractor, "first output should have extractor")

			// Second output: statusCode with StatusCode extractor
			assert.Equal(t, "statusCode", outputs[1].Name, "second output name should be statusCode")
			require.NotNil(t, outputs[1].Extractor, "second output should have extractor")
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

			// Validate assertions
			assertions := reqNode.GetAssertions()
			require.Len(t, assertions, 1, "should have 1 assertion")

			// The assertion should have a StatusCode extractor
			assertion := assertions[0]
			require.NotNil(t, assertion.Extractor, "assertion should have extractor")
			_, isStatusCode := assertion.Extractor.(httpextractors.StatusCodeExtractor)
			assert.True(t, isStatusCode, "assertion extractor should be StatusCodeExtractor")

			// Validate outputs
			outputs := reqNode.GetOutputs()
			require.Len(t, outputs, 1, "should have 1 output")
			assert.Equal(t, "responseStatus", outputs[0].Name, "output name should be responseStatus")
			require.NotNil(t, outputs[0].Extractor, "output should have extractor")
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

			// Validate assertions
			assert.Empty(t, reqNode.GetAssertions(), "should have 0 assertions")

			// Validate outputs
			outputs := reqNode.GetOutputs()
			assert.Empty(t, outputs, "should have 0 outputs")
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

func TestParseFromJSON_ExtractorTypes(t *testing.T) {
	// Test that extractors are properly unmarshaled with correct types
	file, err := os.ReadFile("test.json")
	require.NoError(t, err, "should read test.json file")
	flowResult, err := flow.ParseFromJSON(file)
	require.NoError(t, err, "should parse from json")

	t.Run("JSONPathExtractor", func(t *testing.T) {
		reqNode, ok := node.AsRequestNode(flowResult.Nodes[0])
		require.True(t, ok, "first node should be a RequestNode")

		outputs := reqNode.GetOutputs()
		require.Len(t, outputs, 2, "should have 2 outputs")

		// First output should be JSONPath extractor
		firstOutput := outputs[0]
		assert.Equal(t, "userId", firstOutput.Name, "first output should be userId")
		require.NotNil(t, firstOutput.Extractor, "first output should have extractor")

		// Verify it's a JSONPath extractor by checking type
		jsonPathExt, ok := firstOutput.Extractor.(extractors.JSONPathExtractor)
		require.True(t, ok, "first extractor should be JSONPathExtractor, got type %T", firstOutput.Extractor)
		assert.Equal(t, "$.user.id", jsonPathExt.Path, "JSONPath should match")
	})

	t.Run("StatusCodeExtractor", func(t *testing.T) {
		reqNode, ok := node.AsRequestNode(flowResult.Nodes[0])
		require.True(t, ok, "first node should be a RequestNode")

		outputs := reqNode.GetOutputs()
		require.Len(t, outputs, 2, "should have 2 outputs")

		// Second output should be StatusCode extractor
		secondOutput := outputs[1]
		assert.Equal(t, "statusCode", secondOutput.Name, "second output should be statusCode")
		require.NotNil(t, secondOutput.Extractor, "second output should have extractor")

		// Verify it's a StatusCode extractor by checking type
		_, ok = secondOutput.Extractor.(httpextractors.StatusCodeExtractor)
		require.True(t, ok, "second extractor should be StatusCodeExtractor, got type %T", secondOutput.Extractor)
	})

	t.Run("OutputNames", func(t *testing.T) {
		req1, ok := node.AsRequestNode(flowResult.Nodes[0])
		require.True(t, ok)
		outputs1 := req1.GetOutputs()
		assert.Equal(t, "userId", outputs1[0].Name)
		assert.Equal(t, "statusCode", outputs1[1].Name)

		req2, ok := node.AsRequestNode(flowResult.Nodes[1])
		require.True(t, ok)
		outputs2 := req2.GetOutputs()
		assert.Equal(t, "responseStatus", outputs2[0].Name)

		req3, ok := node.AsRequestNode(flowResult.Nodes[2])
		require.True(t, ok)
		outputs3 := req3.GetOutputs()
		assert.Empty(t, outputs3, "req-error should have no outputs")
	})
}

func TestParseFromJSON_ExtractorFactory(t *testing.T) {
	// Test the extractor factory directly
	t.Run("StatusCodeExtractor", func(t *testing.T) {
		data := []byte(`{"type": "statusCode"}`)
		ext, err := extractors.UnmarshalExtractor(data)
		require.NoError(t, err, "should unmarshal StatusCode extractor")
		require.NotNil(t, ext, "extractor should not be nil")
		_, ok := ext.(httpextractors.StatusCodeExtractor)
		assert.True(t, ok, "should be StatusCodeExtractor, got %T", ext)
	})

	t.Run("JSONPathExtractor", func(t *testing.T) {
		data := []byte(`{"type": "jsonPath", "path": "$.user.id"}`)
		ext, err := extractors.UnmarshalExtractor(data)
		require.NoError(t, err, "should unmarshal JSONPath extractor")
		require.NotNil(t, ext, "extractor should not be nil")
		jsonPathExt, ok := ext.(extractors.JSONPathExtractor)
		assert.True(t, ok, "should be JSONPathExtractor, got %T", ext)
		assert.Equal(t, "$.user.id", jsonPathExt.Path, "path should match")
	})

	t.Run("HeaderExtractor", func(t *testing.T) {
		data := []byte(`{"type": "header", "headerName": "Content-Type"}`)
		ext, err := extractors.UnmarshalExtractor(data)
		require.NoError(t, err, "should unmarshal Header extractor")
		require.NotNil(t, ext, "extractor should not be nil")
		headerExt, ok := ext.(httpextractors.HeaderExtractor)
		assert.True(t, ok, "should be HeaderExtractor, got %T", ext)
		assert.Equal(t, "Content-Type", headerExt.HeaderName, "header name should match")
	})

	t.Run("UnknownExtractor", func(t *testing.T) {
		data := []byte(`{"type": "unknown"}`)
		ext, err := extractors.UnmarshalExtractor(data)
		require.Error(t, err, "should return error for unknown extractor type")
		assert.Nil(t, ext, "extractor should be nil")
		assert.Contains(t, err.Error(), "unknown extractor type")
	})
}

// Import statement for httpextractors (add to imports if not present)
// This is handled by the test code above
