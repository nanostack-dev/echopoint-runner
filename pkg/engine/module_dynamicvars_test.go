package engine_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/dynamicvars"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFlowEngine_Execute_ModuleResolvesDynamicVars is a regression test for the
// bug where nested module executions ran without the dynamic-variable resolver,
// causing {{$runId}} (and other {{$…}}) inside a module's request nodes to
// render literally. That made module-generated names (e.g. the setup-org
// module's product name) identical across runs and thus non-idempotent.
func TestFlowEngine_Execute_ModuleResolvesDynamicVars(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Marker string `json:"marker"`
		}
		_ = json.Unmarshal(raw, &payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"echo":"` + payload.Marker + `"}`))
	}))
	defer server.Close()

	parentJSON := []byte(`{
		"name": "Parent Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "mod",
				"display_name": "Run Module",
				"type": "module",
				"data": {
					"flow_id": "flow-mod",
					"output_bindings": {"seen": "echo-node.seen"}
				}
			}
		],
		"edges": []
	}`)

	childJSON := []byte(`{
		"name": "Module Flow",
		"version": "1.0",
		"nodes": [
			{
				"id": "echo-node",
				"display_name": "Echo Marker",
				"type": "request",
				"data": {
					"method": "POST",
					"url": "` + server.URL + `/echo",
					"body": {"marker": "{{$runId}}"},
					"timeout": 1000
				},
				"outputs": [
					{"name": "seen", "extractor": {"type": "jsonPath", "path": "$.echo"}}
				]
			}
		],
		"edges": []
	}`)

	parentFlow, err := flow.ParseFromJSONWithOptions(parentJSON, flow.ParseOptions{})
	require.NoError(t, err)

	resolver := staticModuleResolver{
		"flow-mod": {FlowDefinition: childJSON},
	}

	dv := dynamicvars.New("exec-module-dynvars-test")
	expectedRunID, err := dv.Resolve("runId", nil)
	require.NoError(t, err)
	require.NotEmpty(t, expectedRunID)

	result, err := engine.ExecuteFlowDefinition(*parentFlow, map[string]any{}, &engine.Options{
		ModuleResolver: resolver,
		DynamicVars:    dv,
	})
	require.NoError(t, err)
	require.True(t, result.Success)

	seen := result.FinalOutputs["mod.seen"]
	assert.Equal(t, expectedRunID, seen,
		"module request node should resolve {{$runId}} to the run's dynamic value")
	assert.NotEqual(t, "{{$runId}}", seen,
		"module request node must not render {{$runId}} literally")
}

// TestFlowEngine_Execute_ModuleUniqueNamesAcrossRuns reproduces the production
// symptom: a module that names a resource with {{$runId}} must produce a
// different name each run. The mock rejects duplicate names with 400, so the
// buggy runner (literal {{$runId}}) collides on the second run; the fix makes
// each run render a distinct runId.
func TestFlowEngine_Execute_ModuleUniqueNamesAcrossRuns(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	var sentNames []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &payload)
		mu.Lock()
		defer mu.Unlock()
		sentNames = append(sentNames, payload.Name)
		if seen[payload.Name] {
			w.WriteHeader(http.StatusBadRequest) // anchor-style duplicate rejection
			_, _ = w.Write([]byte(`{"error":"duplicate name"}`))
			return
		}
		seen[payload.Name] = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"prd_ok"}`))
	}))
	defer server.Close()

	parentJSON := []byte(`{
		"name": "Parent", "version": "1.0",
		"nodes": [{
			"id": "setup", "display_name": "Setup", "type": "module",
			"data": {"flow_id": "flow-setup", "output_bindings": {"productId": "create-product.productId"}}
		}],
		"edges": []
	}`)
	childJSON := []byte(`{
		"name": "Setup Module", "version": "1.0",
		"nodes": [{
			"id": "create-product", "display_name": "Create Product", "type": "request",
			"data": {
				"method": "POST", "url": "` + server.URL + `/products",
				"body": {"name": "eptest-{{$runId}}"}, "timeout": 1000
			},
			"outputs": [{"name": "productId", "extractor": {"type": "jsonPath", "path": "$.id"}}],
			"assertions": [{"extractor_type": "statusCode", "operator_type": "equals", "operator_data": {"value": "201"}, "extractor_data": {}}]
		}],
		"edges": []
	}`)

	parentFlow, err := flow.ParseFromJSONWithOptions(parentJSON, flow.ParseOptions{})
	require.NoError(t, err)
	resolver := staticModuleResolver{"flow-setup": {FlowDefinition: childJSON}}

	// Two independent runs, each with its own runId, like two scheduled runs.
	for _, execID := range []string{"exec-run-1", "exec-run-2"} {
		result, runErr := engine.ExecuteFlowDefinition(*parentFlow, map[string]any{}, &engine.Options{
			ModuleResolver: resolver,
			DynamicVars:    dynamicvars.New(execID),
		})
		require.NoError(t, runErr)
		require.True(t, result.Success,
			"run %s must succeed; a literal runId collides on the second run", execID)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sentNames, 2)
	assert.NotEqual(t, sentNames[0], sentNames[1],
		"each run must send a distinct product name (unique runId)")
	assert.NotContains(t, sentNames[0], "{{$runId}}", "runId must not be literal")
}
