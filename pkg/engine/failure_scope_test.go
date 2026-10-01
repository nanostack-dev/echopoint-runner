package engine_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedCalls struct {
	mu    sync.Mutex
	paths []string
}

func (r *recordedCalls) add(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
}

func (r *recordedCalls) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...)
}

func widgetServer(t *testing.T) (*httptest.Server, *recordedCalls) {
	t.Helper()
	calls := &recordedCalls{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.add(r.Method + " " + r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"wdg_42"}`))
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func TestFlowEngine_Execute_ACleanupDeletesWhatAFailedCreateMade(t *testing.T) {
	server, calls := widgetServer(t)
	definition := []byte(`{
		"name": "Create then clean up",
		"version": "1.0",
		"nodes": [
			{
				"id": "create-widget",
				"display_name": "Create Widget (201)",
				"type": "request",
				"data": {"method": "POST", "url": "` + server.URL + `/widgets", "timeout": 1000},
				"assertions": [{
					"extractor_type": "status_code",
					"extractor_data": {},
					"operator_type": "equals",
					"operator_data": {"value": 201}
				}],
				"outputs": [{"name": "widgetId", "extractor": {"type": "json_path", "path": "$.id"}}]
			},
			{
				"id": "delete-widget",
				"display_name": "Delete Widget",
				"type": "request",
				"run_when": "always",
				"data": {"method": "DELETE", "url": "` + server.URL + `/widgets/{{create-widget.widgetId}}", "timeout": 1000}
			}
		],
		"edges": [{"id": "e1", "source": "create-widget", "target": "delete-widget", "type": "success"}]
	}`)
	parsed, err := flow.ParseFromJSON(definition)
	require.NoError(t, err)

	result, _ := engine.ExecuteFlowDefinition(*parsed, nil, nil)

	require.False(t, result.Success)
	assert.Equal(t, "wdg_42", result.ExecutionResults["create-widget"].GetOutputs()["widgetId"])
	assert.Contains(t, calls.all(), "DELETE /widgets/wdg_42")
}

func TestFlowEngine_Execute_AStepAfterACleanupRunsWhenItsPredecessorsSucceed(t *testing.T) {
	server, calls := widgetServer(t)
	definition := []byte(`{
		"name": "Delete then verify gone",
		"version": "1.0",
		"nodes": [
			{
				"id": "create-widget",
				"display_name": "Create Widget",
				"type": "request",
				"data": {"method": "POST", "url": "` + server.URL + `/widgets", "timeout": 1000},
				"outputs": [{"name": "widgetId", "extractor": {"type": "json_path", "path": "$.id"}}]
			},
			{
				"id": "delete-widget",
				"display_name": "Delete Widget",
				"type": "request",
				"run_when": "always",
				"data": {"method": "DELETE", "url": "` + server.URL + `/widgets/{{create-widget.widgetId}}", "timeout": 1000}
			},
			{
				"id": "verify-gone",
				"display_name": "Verify Gone",
				"type": "request",
				"data": {"method": "GET", "url": "` + server.URL + `/widgets/{{create-widget.widgetId}}", "timeout": 1000}
			}
		],
		"edges": [
			{"id": "e1", "source": "create-widget", "target": "delete-widget", "type": "success"},
			{"id": "e2", "source": "delete-widget", "target": "verify-gone", "type": "success"}
		]
	}`)
	parsed, err := flow.ParseFromJSON(definition)
	require.NoError(t, err)

	result, err := engine.ExecuteFlowDefinition(*parsed, nil, nil)

	require.NoError(t, err)
	require.True(t, result.Success)
	assert.Contains(t, result.ExecutionResults, "verify-gone")
	assert.Equal(t, []string{"POST /widgets", "DELETE /widgets/wdg_42", "GET /widgets/wdg_42"}, calls.all())
}
