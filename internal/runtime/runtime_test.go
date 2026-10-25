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
	"sync/atomic"
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

func TestClaimLoopLogsEdgeNotFoundAtWarnUntilItPersists(t *testing.T) {
	statuses := repeatStatus(http.StatusNotFound, claimFailureEscalationThreshold)

	logs := runClaimLoopAgainst(t, statuses)

	expected := append(repeatLevel("warn", claimFailureEscalationThreshold-1), "error")
	assert.Equal(t, expected, levelsOf(t, logs, "failed to claim runner job"))
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

func TestClaimedJobLimitFailsTheCallPastIt(t *testing.T) {
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
	assert.Equal(t, 1, job.MaxHTTPCallsPerExecution)
	r := newRuntime(testConfig(server.URL), controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL}))

	outcome, err := r.executeClaimedJob(context.Background(), &activeJob{job: &job, startedAt: time.Now()})

	require.NoError(t, err)
	assert.Equal(t, jobStatusFailed, outcome.Status)
	require.NotNil(t, completion.ErrorMessage)
	assert.Equal(t, "The execution reached its limit of 1 HTTP calls", *completion.ErrorMessage)
}

func TestRejectedLeaseStopsTheRunBeforeItsNextRequest(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()

	jobID := uuid.New()
	var completions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/heartbeat"):
			_, _ = w.Write([]byte(`[{"job_id":"` + jobID.String() + `","status":"rejected"}]`))
		case strings.HasSuffix(r.URL.Path, "/complete"):
			completions.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	config := testConfig(server.URL)
	config.HeartbeatInterval = 20 * time.Millisecond
	job := &controlplane.ClaimedJob{
		JobID:       jobID,
		ExecutionID: uuid.New(),
		FlowDefinition: json.RawMessage(`{
			"name": "Wait then call", "version": "1.0",
			"nodes": [
				{"id": "wait", "type": "delay", "data": {"duration": 10000}},
				{"id": "call", "type": "request", "data": {"method": "POST", "url": "` + target.URL + `/orders"}}
			],
			"edges": [{"id": "e", "source": "wait", "target": "call"}]
		}`),
		Inputs: map[string]any{},
	}

	started := time.Now()
	outcome, err := RunOne(context.Background(), config, "job-token", uuid.New(), job)

	require.Error(t, err)
	assert.Equal(t, jobStatusFailed, outcome.Status)
	assert.Less(t, time.Since(started), 5*time.Second)
	assert.Zero(t, targetHits.Load())
	assert.Zero(t, completions.Load())
}

func TestClaimedJobFailsAResponsePastTheConfiguredMaxResponseBytes(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 2048)))
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
	config := testConfig(server.URL)
	config.MaxResponseBytes = 1024
	job := &controlplane.ClaimedJob{
		JobID:       uuid.New(),
		ExecutionID: uuid.New(),
		FlowDefinition: json.RawMessage(`{
			"name": "Download", "version": "1.0", "edges": [],
			"nodes": [{"id": "download", "type": "request", "data": {"method": "GET", "url": "` + target.URL + `"}}]
		}`),
		Inputs: map[string]any{},
	}
	r := newRuntime(config, controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL}))

	outcome, err := r.executeClaimedJob(context.Background(), &activeJob{job: job, startedAt: time.Now()})

	require.NoError(t, err)
	assert.Equal(t, jobStatusFailed, outcome.Status)
	require.NotNil(t, completion.ErrorMessage)
	assert.Contains(t, *completion.ErrorMessage, "larger than the 1024 bytes limit")
}

func TestJobFailedByTargetStreamRefusalLogsAtWarn(t *testing.T) {
	logs := captureLogs(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/complete") {
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
			"name": "Stream", "version": "1.0", "edges": [],
			"nodes": [{"id": "stream", "type": "sse", "data": {"url": "` + target.URL + `", "timeout_ms": 2000}}]
		}`),
		Inputs: map[string]any{},
	}

	outcome, err := r.executeClaimedJob(context.Background(), &activeJob{job: job, startedAt: time.Now()})

	require.NoError(t, err)
	assert.Equal(t, jobStatusFailed, outcome.Status)
	assert.Equal(t, []string{"warn"}, levelsOf(t, logs, "runner job failed"))
	assert.NotContains(t, levelsOf(t, logs, "Node execution failed"), "error")
}

func TestConcurrentJobsShareRequestPacingIncludingRedirectsAndSSE(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {}\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cfg := testConfig(server.URL)
	cfg.MsBetweenRequests = 100
	r := newRuntime(cfg, controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL}))
	var workers sync.WaitGroup
	for range 2 {
		job := &controlplane.ClaimedJob{
			JobID: uuid.New(), ExecutionID: uuid.New(), Inputs: map[string]any{},
			FlowDefinition: json.RawMessage(`{
    "name":"Paced requests", "version":"1.0", "edges":[], "nodes":[
     {"id":"redirect","type":"request","data":{"method":"GET","url":"` + target.URL + `/redirect"}},
     {"id":"stream","type":"sse","data":{"url":"` + target.URL + `/stream","max_events":1,"timeout_ms":2000}}
    ]
   }`),
		}
		workers.Go(func() {
			outcome, err := r.executeClaimedJob(context.Background(), &activeJob{job: job, startedAt: time.Now()})
			if err != nil {
				t.Error(err)
				return
			}
			assert.Equal(t, jobStatusCompleted, outcome.Status)
		})
	}
	workers.Wait()
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, starts, 6)
	for i := 1; i < len(starts); i++ {
		// Server arrival includes network scheduling; allow 10ms of jitter.
		assert.GreaterOrEqual(t, starts[i].Sub(starts[i-1]), 90*time.Millisecond)
	}
}

// The claim's trace context joins the runner's job callbacks to the claim's trace.
// A request node calls a customer target, which must never receive it.
func TestJobCallbacksCarryTheTraceContextAndTargetRequestsDoNot(t *testing.T) {
	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	var mu sync.Mutex
	var targetHeaders http.Header
	callbackHeaders := map[string]http.Header{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		targetHeaders = r.Header.Clone()
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		mu.Lock()
		callbackHeaders[kind] = r.Header.Clone()
		mu.Unlock()
		switch kind {
		case "complete":
			w.WriteHeader(http.StatusNoContent)
		case "webhook-requests":
			_, _ = w.Write([]byte(`{"items":[{"id":"1","method":"POST","body":"{\"event\":\"done\"}",` +
				`"received_at":"2026-10-24T12:00:00Z"}]}`))
		default:
			_, _ = w.Write([]byte(`{"last_accepted_sequence":100}`))
		}
	}))
	defer server.Close()

	claim := `{
		"job_id": "` + uuid.NewString() + `",
		"execution_id": "` + uuid.NewString() + `",
		"job_token": "job-token",
		"trace_context": {"traceparent": "` + traceparent + `", "tracestate": "echopoint=1"},
		"inputs": {"webhook.requests_url": "` + server.URL + `/webhook-requests"},
		"flow_definition": {
			"name": "Traced", "version": "1.0",
			"nodes": [
				{"id": "call", "type": "request", "data": {"method": "GET", "url": "` + target.URL + `/a"}},
				{"id": "wait", "type": "webhook_wait", "data": {"timeout_ms": 2000}, "assertions": [
					{"extractor_type": "json_path", "extractor_data": {"path": "$.event"},
					 "operator_type": "equals", "operator_data": {"value": "done"}}]}
			],
			"edges": [{"id": "e", "source": "call", "target": "wait"}]
		}
	}`
	var job controlplane.ClaimedJob
	require.NoError(t, json.Unmarshal([]byte(claim), &job))
	r := newRuntime(testConfig(server.URL), controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL}))

	outcome, err := r.executeClaimedJob(context.Background(), &activeJob{job: &job, startedAt: time.Now()})

	require.NoError(t, err)
	require.Equal(t, jobStatusCompleted, outcome.Status, outcome.ErrorMessage)
	mu.Lock()
	defer mu.Unlock()
	for _, kind := range []string{"events", "complete", "webhook-requests"} {
		require.Contains(t, callbackHeaders, kind)
		assert.Equal(t, traceparent, callbackHeaders[kind].Get("Traceparent"), kind)
		assert.Equal(t, "echopoint=1", callbackHeaders[kind].Get("Tracestate"), kind)
	}
	require.NotNil(t, targetHeaders)
	assert.Empty(t, targetHeaders.Get("Traceparent"))
	assert.Empty(t, targetHeaders.Get("Tracestate"))
}
