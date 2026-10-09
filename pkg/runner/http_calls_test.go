package runner_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/httpcall"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func statusServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func parseFlow(t *testing.T, definition string, inputKeys ...string) flow.Flow {
	t.Helper()
	parsed, err := flow.ParseFromJSONWithOptions(
		[]byte(definition),
		flow.ParseOptions{AllowedInitialInputKeys: inputKeys},
	)
	require.NoError(t, err)
	return *parsed
}

func requestNode(id, url string) string {
	return fmt.Sprintf(`{"id":%q,"type":"request","data":{"method":"get","url":%q}}`, id, url)
}

func TestRun_RecordsEachHTTPCallWithoutResolvedValues(t *testing.T) {
	server := statusServer(t)
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[`+
		requestNode("ok", "{{baseUrl}}/users/{{ userId }}?token={{secret}}")+`,`+
		requestNode("missing", "{{baseUrl}}/missing")+
		`],"edges":[{"id":"e","source":"ok","target":"missing"}]}`, "baseUrl", "userId", "secret")

	result, _ := runner.Run(flowDef, map[string]any{
		"baseUrl": server.URL, "userId": "42", "secret": "s3cr3t",
	}, runner.WithSecretInputKeys([]string{"secret"}))

	require.Len(t, result.HTTPCalls, 2)
	first, second := result.HTTPCalls[0], result.HTTPCalls[1]
	assert.Equal(t, 1, first.Seq)
	assert.Equal(t, "ok", first.NodeID)
	assert.Equal(t, "GET", first.Method)
	assert.Equal(t, server.Listener.Addr().String(), first.Host)
	assert.Equal(t, "/users/{userId}", first.PathTemplate)
	assert.Equal(t, http.StatusOK, *first.StatusCode)
	assert.Empty(t, first.ErrorClass)
	assert.Equal(t, int64(len(`{"ok":true}`)), *first.ResponseBytes)

	assert.Equal(t, "missing", second.NodeID)
	assert.Equal(t, http.StatusNotFound, *second.StatusCode)
	assert.Equal(t, spi.HTTPCallError4xx, second.ErrorClass)
}

func TestRun_RecordsTransportFailureClass(t *testing.T) {
	server := statusServer(t)
	unreachable := server.URL
	server.Close()
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[`+requestNode("down", unreachable)+`],"edges":[]}`)

	result, err := runner.Run(flowDef, map[string]any{})

	require.Error(t, err)
	require.Len(t, result.HTTPCalls, 1)
	assert.Nil(t, result.HTTPCalls[0].StatusCode)
	assert.Equal(t, spi.HTTPCallErrorConnect, result.HTTPCalls[0].ErrorClass)
}

func TestRun_CountsLoopIterationCalls(t *testing.T) {
	server := statusServer(t)
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[{"id":"each","type":"loop","data":{
		"items":[1,2,3],
		"body":{"nodes":[`+requestNode("get", server.URL+"/items/{{item}}")+`],"edges":[]}
	}}],"edges":[]}`)

	result, err := runner.Run(flowDef, map[string]any{})

	require.NoError(t, err)
	require.Len(t, result.HTTPCalls, 3)
	for i, call := range result.HTTPCalls {
		assert.Equal(t, i+1, call.Seq)
		assert.Equal(t, "get", call.NodeID)
		assert.Equal(t, "/items/{item}", call.PathTemplate)
	}
}

func TestRun_HTTPCallLimitFailsTheNodePastIt(t *testing.T) {
	server := statusServer(t)
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[{"id":"each","type":"loop","data":{
		"items":[1,2,3],
		"body":{"nodes":[`+requestNode("get", server.URL+"/items/{{item}}")+`],"edges":[]}
	}}],"edges":[]}`)

	result, err := runner.Run(flowDef, map[string]any{}, runner.WithHTTPCallLimit(2))

	require.Error(t, err)
	assert.Len(t, result.HTTPCalls, 2, "the refused call is never sent, so it is not recorded")
	userErr, ok := spi.AsUserError(err)
	require.True(t, ok, "expected a user error, got %v", err)
	assert.Equal(t, httpcall.LimitExceededCode, userErr.Code)
}
