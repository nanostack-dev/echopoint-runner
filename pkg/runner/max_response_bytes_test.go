package runner_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
)

func TestWithMaxResponseBytesFailsAStreamedResponsePastTheLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 600)))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(strings.Repeat("a", 600)))
	}))
	defer server.Close()
	definition := parseFlow(t, `{"version":"1.0","name":"t","nodes":[`+
		requestNode("download", server.URL)+`],"edges":[]}`)

	result, err := runner.Run(definition, nil, runner.WithMaxResponseBytes(1024))

	require.Error(t, err)
	encoded, err := json.Marshal(result.ExecutionResults["download"])
	require.NoError(t, err)
	var download struct {
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	}
	require.NoError(t, json.Unmarshal(encoded, &download))
	assert.Equal(t, "RESPONSE_TOO_LARGE", download.ErrorCode)
	assert.Contains(t, download.ErrorMessage, "1024 bytes")
}
