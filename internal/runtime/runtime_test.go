//nolint:testpackage // white-box: drives the unexported claim loop and job executor
package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	configpkg "github.com/nanostack-dev/echopoint-runner/internal/config"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func levelsOf(t *testing.T, logs *bytes.Buffer, message string) []string {
	t.Helper()
	var levels []string
	scanner := bufio.NewScanner(strings.NewReader(logs.String()))
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		if line["message"] == message {
			levels = append(levels, line["level"].(string))
		}
	}
	return levels
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	logs := &bytes.Buffer{}
	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	//nolint:reassign // capturing the global logger is the point of this helper
	log.Logger = zerolog.New(zerolog.SyncWriter(logs))
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		//nolint:reassign // restores what the helper replaced
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})
	return logs
}

func testConfig(baseURL string) configpkg.Config {
	return configpkg.Config{
		BaseURL:           baseURL,
		RunnerID:          "runner",
		MaxParallelFlows:  1,
		HeartbeatInterval: time.Hour,
		RequestTimeout:    5 * time.Second,
		IdleBackoff:       time.Millisecond,
		ErrorBackoff:      time.Millisecond,
	}
}

func repeatLevel(level string, count int) []string {
	levels := make([]string, count)
	for i := range levels {
		levels[i] = level
	}
	return levels
}

func runClaimLoopAgainst(t *testing.T, claimStatuses []int) *bytes.Buffer {
	t.Helper()
	logs := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	claims := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if claims >= len(claimStatuses) {
			cancel()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		status := claimStatuses[claims]
		claims++
		w.WriteHeader(status)
		if status != http.StatusNoContent {
			_, _ = w.Write([]byte("error code: 502"))
		}
	}))
	defer server.Close()

	r := newRuntime(testConfig(server.URL), controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL}))
	require.ErrorIs(t, r.runClaimLoop(ctx), context.Canceled)
	return logs
}

func TestClaimLoopEscalatesTransientFailuresAfterThreshold(t *testing.T) {
	statuses := repeatStatus(http.StatusBadGateway, claimFailureEscalationThreshold+1)

	logs := runClaimLoopAgainst(t, statuses)

	expected := append(repeatLevel("warn", claimFailureEscalationThreshold-1), "error", "error")
	assert.Equal(t, expected, levelsOf(t, logs, "failed to claim runner job"))
}

func TestClaimLoopResetsFailureCountAfterAnAnsweredPoll(t *testing.T) {
	statuses := append(repeatStatus(http.StatusBadGateway, claimFailureEscalationThreshold-1), http.StatusNoContent)
	statuses = append(statuses, repeatStatus(http.StatusBadGateway, claimFailureEscalationThreshold-1)...)

	logs := runClaimLoopAgainst(t, statuses)

	assert.Equal(t,
		repeatLevel("warn", 2*(claimFailureEscalationThreshold-1)),
		levelsOf(t, logs, "failed to claim runner job"))
}

func TestClaimLoopLogsRejectedRunnerKeyAtErrorImmediately(t *testing.T) {
	logs := runClaimLoopAgainst(t, []int{http.StatusUnauthorized})

	assert.Equal(t, []string{"error"}, levelsOf(t, logs, "failed to claim runner job"))
}

func repeatStatus(status, count int) []int {
	statuses := make([]int, count)
	for i := range statuses {
		statuses[i] = status
	}
	return statuses
}

func TestJobWithUnknownInitialVariableFailsAtWarn(t *testing.T) {
	logs := captureLogs(t)
	var completion controlplane.CompleteJobRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/complete") {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&completion))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	r := newRuntime(testConfig(server.URL), controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL}))
	job := &controlplane.ClaimedJob{
		JobID:       uuid.New(),
		ExecutionID: uuid.New(),
		FlowDefinition: json.RawMessage(`{
			"name": "Products", "version": "1.0", "edges": [],
			"nodes": [{
				"id": "create-product", "type": "request",
				"data": {"method": "GET", "url": "{{nanostackBaseUrl}}/products"},
				"inputs": ["nanostackBaseUrl"]
			}]
		}`),
		Inputs: map[string]any{},
	}

	outcome, err := r.executeClaimedJob(context.Background(), &activeJob{job: job, startedAt: time.Now()})

	require.NoError(t, err)
	assert.Equal(t, jobStatusFailed, outcome.Status)
	require.NotNil(t, completion.ErrorMessage)
	assert.Contains(t, *completion.ErrorMessage, "references unknown initial variable 'nanostackBaseUrl'")
	assert.Equal(t, []string{"warn"}, levelsOf(t, logs, "runner job failed"))
}

func TestFailedJobReportsItsHTTPCallsPastTheLimit(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	var completion controlplane.CompleteJobRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/complete") {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&completion))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	claim := `{
		"job_id": "` + uuid.NewString() + `",
		"execution_id": "` + uuid.NewString() + `",
		"max_http_calls_per_execution": 1,
		"inputs": {},
		"flow_definition": {
			"name": "Two calls", "version": "1.0",
			"nodes": [
				{"id": "first", "type": "request", "data": {"method": "GET", "url": "` + target.URL + `/a"}},
				{"id": "second", "type": "request", "data": {"method": "GET", "url": "` + target.URL + `/b"}}
			],
			"edges": [{"id": "e", "source": "first", "target": "second"}]
		}
	}`
	var job controlplane.ClaimedJob
	require.NoError(t, json.Unmarshal([]byte(claim), &job))
	r := newRuntime(testConfig(server.URL), controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL}))

	outcome, err := r.executeClaimedJob(context.Background(), &activeJob{job: &job, startedAt: time.Now()})

	require.NoError(t, err)
	assert.Equal(t, jobStatusFailed, outcome.Status)
	require.NotNil(t, completion.ErrorMessage)
	assert.Equal(t, "The execution reached its limit of 1 HTTP calls", *completion.ErrorMessage)
	require.NotNil(t, completion.Result, "a failed run must still report its partial result")
	calls, ok := (*completion.Result)["http_calls"].([]any)
	require.True(t, ok, "http_calls missing from %v", *completion.Result)
	require.Len(t, calls, 1)
	assert.Equal(t, "first", calls[0].(map[string]any)["node_id"])
}
