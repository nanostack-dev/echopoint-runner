package engine_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func customerServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Location", "/customers/cus_42")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// createThenRead posts to serverURL and hands the given variables, which read
// the response of create_customer, to a set_variable step.
func createThenRead(serverURL, createOutputs, variables string) string {
	return `{
		"name": "f", "version": "1.0",
		"nodes": [
			{"id": "create_customer", "type": "request",
				"data": {"method": "POST", "url": "` + serverURL + `/customers", "timeout": 1000},
				"outputs": ` + createOutputs + `},
			{"id": "read", "type": "set_variable", "data": {"variables": ` + variables + `}}
		],
		"edges": [{"id": "e1", "source": "create_customer", "target": "read"}]
	}`
}

func runFlowJSON(t *testing.T, flowJSON string) *spi.FlowExecutionResult {
	t.Helper()
	parsed, err := flow.ParseFromJSONWithOptions([]byte(flowJSON), flow.ParseOptions{})
	require.NoError(t, err)
	flowEngine, err := engine.NewFlowEngine(*parsed, &engine.Options{})
	require.NoError(t, err)
	result, err := flowEngine.Execute(nil)
	require.NoError(t, err)
	require.True(t, result.Success, "flow failed: %v", result.Error)
	return result
}

func TestARequestExposesItsBodyStatusAndHeadersWithoutDeclaredOutputs(t *testing.T) {
	server := customerServer(t, "application/json", `{"data":{"id":"cus_42"}}`)

	result := runFlowJSON(t, createThenRead(server.URL, `[]`, `{
		"id": "{{create_customer.body.data.id}}",
		"status": "{{{create_customer.status}}}",
		"location": "{{create_customer.headers.Location}}"
	}`))

	assert.Equal(t, map[string]any{
		"id":       "cus_42",
		"status":   http.StatusCreated,
		"location": "/customers/cus_42",
	}, result.ExecutionResults["read"].GetOutputs())
}

func TestADeclaredOutputReplacesTheResponseOutputOfTheSameName(t *testing.T) {
	server := customerServer(t, "application/json", `{"data":{"id":"cus_42"}}`)

	result := runFlowJSON(t, createThenRead(server.URL,
		`[{"name": "body", "extractor": {"type": "json_path", "path": "$.data"}}]`,
		`{"id": "{{create_customer.body.id}}"}`))

	assert.Equal(t, "cus_42", result.ExecutionResults["read"].GetOutputs()["id"])
}

func TestATextResponseBodyIsExposedAsAString(t *testing.T) {
	server := customerServer(t, "text/plain", "created cus_42")

	result := runFlowJSON(t, createThenRead(server.URL, `[]`, `{"text": "{{{create_customer.body}}}"}`))

	assert.Equal(t, "created cus_42", result.ExecutionResults["read"].GetOutputs()["text"])
}

func TestResponseOutputsAreNotStoredAsOutputs(t *testing.T) {
	server := customerServer(t, "application/json", `{"data":{"id":"cus_42"}}`)

	result := runFlowJSON(t, createThenRead(server.URL,
		`[{"name": "customer_id", "extractor": {"type": "json_path", "path": "$.data.id"}}]`,
		`{"id": "{{create_customer.body.data.id}}"}`))

	assert.Equal(t, map[string]any{"customer_id": "cus_42"},
		result.ExecutionResults["create_customer"].GetOutputs())
	assert.Equal(t, map[string]any{
		"create_customer.customer_id": "cus_42",
		"read.id":                     "cus_42",
	}, result.FinalOutputs)
}

func TestALoopDoesNotCollectTheResponsesOfItsSteps(t *testing.T) {
	server := customerServer(t, "application/json", `{"data":{"id":"cus_42"}}`)

	result := runFlowJSON(t, `{
		"name": "f", "version": "1.0",
		"nodes": [{"id": "loop", "type": "loop", "data": {
			"items": ["a"],
			"body": {"nodes": [
				{"id": "create_customer", "type": "request",
					"data": {"method": "POST", "url": "`+server.URL+`/customers", "timeout": 1000}},
				{"id": "read", "type": "set_variable",
					"data": {"variables": {"id": "{{create_customer.body.data.id}}"}}}
			], "edges": [{"id": "e1", "source": "create_customer", "target": "read"}]}
		}}],
		"edges": []
	}`)

	assert.Equal(t, []map[string]any{{"read.id": "cus_42"}},
		result.ExecutionResults["loop"].GetOutputs()["results"])
}

func TestAReferenceToAnUnknownResponseOutputIsRejected(t *testing.T) {
	_, err := flow.ParseFromJSONWithOptions([]byte(createThenRead("http://localhost", `[]`,
		`{"id": "{{create_customer.payload.id}}"}`)), flow.ParseOptions{})

	require.ErrorContains(t, err, "output 'payload.id' not declared by source node 'create_customer'")
}

// order declares status from the body; the body has none, so the node fails.
// An always-run step reading {{order.status}} must find it missing, not the
// HTTP status the response output would have given.
func TestADeclaredOutputThatFailsToExtractStillReplacesTheResponseOutput(t *testing.T) {
	server := customerServer(t, "application/json", `{"data":{"id":"cus_42"}}`)
	parsed, err := flow.ParseFromJSONWithOptions([]byte(`{
		"name": "f", "version": "1.0",
		"nodes": [
			{"id": "order", "type": "request",
				"data": {"method": "POST", "url": "`+server.URL+`/orders", "timeout": 1000},
				"outputs": [{"name": "status", "extractor": {"type": "json_path", "path": "$.status"}}]},
			{"id": "report", "type": "set_variable", "run_when": "always",
				"data": {"variables": {"status": "{{order.status}}"}}}
		],
		"edges": [{"id": "e1", "source": "order", "target": "report"}]
	}`), flow.ParseOptions{})
	require.NoError(t, err)
	flowEngine, err := engine.NewFlowEngine(*parsed, &engine.Options{})
	require.NoError(t, err)

	result, _ := flowEngine.Execute(nil)

	require.Contains(t, result.ExecutionResults, "report")
	assert.NotContains(t, result.ExecutionResults["report"].GetOutputs(), "status")
}
