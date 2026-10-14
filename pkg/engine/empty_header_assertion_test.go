package engine_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/stretchr/testify/require"
)

// A response header that is present with an empty value passes an `empty`
// header assertion instead of failing as not found.
func TestEmptyResponseHeaderPassesEmptyAssertion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["X-Empty"] = []string{""}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parsed, err := flow.ParseFromJSON([]byte(`{
		"name": "f", "version": "1.0",
		"nodes": [{"id": "get", "type": "request",
			"data": {"method": "GET", "url": "` + server.URL + `/headers", "timeout": 1000},
			"assertions": [{"extractor_type": "header", "extractor_data": {"header_name": "X-Empty"},
				"operator_type": "empty", "operator_data": {}}]}],
		"edges": []
	}`))
	require.NoError(t, err)

	result, err := engine.ExecuteFlowDefinition(*parsed, map[string]any{}, &engine.Options{})

	require.NoError(t, err)
	require.True(t, result.Success)
}
