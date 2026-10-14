package runner_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
)

// A poll's timeout_ms bounds the whole poll, including a body attempt that is
// still running when the deadline passes.
func TestRun_PollTimeoutCancelsARunningBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(3 * time.Second):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"ready"}`))
	}))
	t.Cleanup(server.Close)
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[{"id":"wait","type":"poll",
		"assertions":[{"extractor_type":"json_path","extractor_data":{"path":"$['slow.state']"},
			"operator_type":"equals","operator_data":{"value":"ready"}}],
		"data":{"max_attempts":2,"interval_ms":1,"timeout_ms":200,"body":{"nodes":[
			{"id":"slow","type":"request","data":{"method":"GET","url":"`+server.URL+`/delay"},
			 "outputs":[{"name":"state","extractor":{"type":"json_path","path":"$.state"}}]}
		],"edges":[]}}}],"edges":[]}`)

	started := time.Now()
	result, err := runner.Run(flowDef, map[string]any{})
	elapsed := time.Since(started)

	require.Error(t, err, "the poll should not complete after its timeout")
	require.NotNil(t, result)
	wait, ok := result.ExecutionResults["wait"]
	require.True(t, ok, "the poll node should report a result")
	encoded := encodeJSON(t, wait)
	assert.Contains(t, encoded, `"error_code":"POLL_CANCELLED"`)
	assert.Less(t, elapsed, 2*time.Second, "the poll should stop near its 200 ms timeout")
}
