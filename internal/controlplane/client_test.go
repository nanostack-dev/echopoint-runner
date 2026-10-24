package controlplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunnerClientUsesAPIKeyWithoutJobToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "runner-key", r.Header.Get("X-Api-Key"))
		assert.Equal(t, "organization", r.Header.Get("X-Organization-Id"))
		assert.Empty(t, r.Header.Get("X-Job-Token"))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := controlplane.NewRunnerClient(controlplane.Config{
		BaseURL: server.URL, OrganizationID: "organization", RunnerAPIKey: "runner-key",
	})
	_, err := client.Heartbeat(context.Background(), controlplane.HeartbeatRequest{})
	require.NoError(t, err)
}

func TestClaimedJob_UnmarshalReferencedFlows(t *testing.T) {
	payload := []byte(`{
		"job_id": "0195d8a3-d120-7a4f-9bc1-97ddf4b72eb7",
		"execution_id": "0195d8a3-d121-7a4f-9bc1-97ddf4b72eb7",
		"flow_id": "0195d8a3-d122-7a4f-9bc1-97ddf4b72eb7",
		"lease_expires_at": "2026-04-27T12:00:00Z",
		"flow_definition": {"name": "Parent", "version": "1.0", "nodes": [], "edges": []},
		"inputs": {"BASE_URL": "https://api.example.com", "RETRY_COUNT": 3},
		"referenced_flows": {
			"flow-charge": {
				"flow_definition": {"name": "Child", "version": "1.0", "nodes": [], "edges": []},
				"input_overrides": {"TOKEN": "secret", "TIMEOUT_MS": 1000}
			}
		}
	}`)

	var claimedJob controlplane.ClaimedJob
	err := json.Unmarshal(payload, &claimedJob)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", claimedJob.Inputs["BASE_URL"])
	assert.EqualValues(t, 3, claimedJob.Inputs["RETRY_COUNT"])
	require.Contains(t, claimedJob.ReferencedFlows, "flow-charge")
	assert.JSONEq(
		t,
		`{"name":"Child","version":"1.0","nodes":[],"edges":[]}`,
		string(claimedJob.ReferencedFlows["flow-charge"].FlowDefinition),
	)
	assert.Equal(t, "secret", claimedJob.ReferencedFlows["flow-charge"].InputOverrides["TOKEN"])
	assert.EqualValues(t, 1000, claimedJob.ReferencedFlows["flow-charge"].InputOverrides["TIMEOUT_MS"])
}

func TestClaimedJob_UnmarshalReferencedFlowsLegacyEnvironmentFallback(t *testing.T) {
	payload := []byte(`{
		"job_id": "0195d8a3-d120-7a4f-9bc1-97ddf4b72eb7",
		"execution_id": "0195d8a3-d121-7a4f-9bc1-97ddf4b72eb7",
		"flow_id": "0195d8a3-d122-7a4f-9bc1-97ddf4b72eb7",
		"lease_expires_at": "2026-04-27T12:00:00Z",
		"flow_definition": {"name": "Parent", "version": "1.0", "nodes": [], "edges": []},
		"referenced_flows": {
			"flow-charge": {
				"flow_definition": {"name": "Child", "version": "1.0", "nodes": [], "edges": []},
				"environment": {"TOKEN": "secret"}
			}
		}
	}`)

	var claimedJob controlplane.ClaimedJob
	err := json.Unmarshal(payload, &claimedJob)
	require.NoError(t, err)
	assert.Equal(t, "secret", claimedJob.ReferencedFlows["flow-charge"].InputOverrides["TOKEN"])
}

func TestClaimedJob_UnmarshalLegacyEnvironmentFallback(t *testing.T) {
	payload := []byte(`{
		"job_id": "0195d8a3-d120-7a4f-9bc1-97ddf4b72eb7",
		"execution_id": "0195d8a3-d121-7a4f-9bc1-97ddf4b72eb7",
		"flow_id": "0195d8a3-d122-7a4f-9bc1-97ddf4b72eb7",
		"lease_expires_at": "2026-04-27T12:00:00Z",
		"flow_definition": {"name": "Parent", "version": "1.0", "nodes": [], "edges": []},
		"environment": {"BASE_URL": "https://api.example.com", "TOKEN": "root-token"}
	}`)

	var claimedJob controlplane.ClaimedJob
	err := json.Unmarshal(payload, &claimedJob)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", claimedJob.Inputs["BASE_URL"])
	assert.Equal(t, "root-token", claimedJob.Inputs["TOKEN"])
}

func TestClaimedJobKeepsTheSelfHostedJobToken(t *testing.T) {
	var job controlplane.ClaimedJob
	raw := `{"job_id":"0192f0d6-0000-7000-8000-000000000001","inputs":{},"job_token":"jt-self-hosted"}`
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.JobToken != "jt-self-hosted" {
		t.Fatalf("expected the claim job token, got %q", job.JobToken)
	}
}

func claimErrorForStatus(t *testing.T, status int) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("error code: " + http.StatusText(status)))
	}))
	defer server.Close()

	client := controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL})
	_, err := client.ClaimNext(context.Background(), controlplane.ClaimNextRequest{})
	require.Error(t, err)
	return err
}

func TestIsTransientForGatewayErrorDuringRedeploy(t *testing.T) {
	assert.True(t, controlplane.IsTransient(claimErrorForStatus(t, http.StatusBadGateway)))
}

func TestIsTransientForRateLimit(t *testing.T) {
	assert.True(t, controlplane.IsTransient(claimErrorForStatus(t, http.StatusTooManyRequests)))
}

func TestIsTransientForEdgeNotFoundWhileRoutesChurn(t *testing.T) {
	assert.True(t, controlplane.IsTransient(claimErrorForStatus(t, http.StatusNotFound)))
}

func TestIsNotTransientForRejectedRunnerKey(t *testing.T) {
	assert.False(t, controlplane.IsTransient(claimErrorForStatus(t, http.StatusUnauthorized)))
	assert.False(t, controlplane.IsTransient(claimErrorForStatus(t, http.StatusForbidden)))
}

func TestIsTransientForRefusedConnection(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	baseURL := server.URL
	server.Close()

	client := controlplane.NewRunnerClient(controlplane.Config{BaseURL: baseURL})
	_, err := client.ClaimNext(context.Background(), controlplane.ClaimNextRequest{})
	require.Error(t, err)
	assert.True(t, controlplane.IsTransient(err))
}

// The control plane refuses a job whose flow declares a secret output to a
// runner that does not declare it can mask one, so every claim declares it.
func TestClaimNextDeclaresTheSecretOutputsCapability(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL})
	_, err := client.ClaimNext(context.Background(), controlplane.ClaimNextRequest{RunnerID: "runner-1"})

	require.ErrorIs(t, err, controlplane.ErrNoJobAvailable)
	assert.Equal(t, "runner-1", body["runner_id"])
	assert.Equal(t, []any{"secret_outputs"}, body["capabilities"])
}

const testTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func TestClaimedJobDecodesTheTraceContext(t *testing.T) {
	var job controlplane.ClaimedJob
	require.NoError(t, json.Unmarshal([]byte(`{"trace_context":{"traceparent":"`+testTraceparent+
		`","tracestate":"echopoint=1"}}`), &job))
	assert.Equal(t, spi.TraceContext{Traceparent: testTraceparent, Tracestate: "echopoint=1"}, job.TraceContext)
}

func TestClaimedJobFromAnOldServerHasNoTraceContext(t *testing.T) {
	var job controlplane.ClaimedJob
	require.NoError(t, json.Unmarshal([]byte(`{"inputs":{}}`), &job))
	assert.Equal(t, spi.TraceContext{}, job.TraceContext)
}

func TestClaimedJobDropsAnInvalidTraceparent(t *testing.T) {
	var job controlplane.ClaimedJob
	require.NoError(t, json.Unmarshal([]byte(
		`{"trace_context":{"traceparent":"not-a-trace","tracestate":"echopoint=1"}}`), &job))
	assert.Equal(t, spi.TraceContext{}, job.TraceContext)
}

func TestJobCallsSendTheTraceContextAndHeartbeatDoesNot(t *testing.T) {
	headers := map[string]http.Header{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers[r.URL.Path] = r.Header.Clone()
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/complete"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/heartbeat"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"last_accepted_sequence":0}`))
		}
	}))
	defer server.Close()
	client := controlplane.NewJobClient(server.URL, "job-token", 5*time.Second)
	jobID := uuid.New()
	traced := spi.WithTraceContext(context.Background(),
		spi.TraceContext{Traceparent: testTraceparent, Tracestate: "echopoint=1"})

	_, err := client.SendJobEvents(traced, jobID, controlplane.SendJobEventsRequest{})
	require.NoError(t, err)
	require.NoError(t, client.Complete(traced, jobID, controlplane.CompleteJobRequest{}))
	_, err = client.Heartbeat(context.Background(), controlplane.HeartbeatRequest{})
	require.NoError(t, err)

	for _, path := range []string{"/runner/jobs/" + jobID.String() + "/events", "/runner/jobs/" + jobID.String() + "/complete"} {
		assert.Equal(t, testTraceparent, headers[path].Get("Traceparent"), path)
		assert.Equal(t, "echopoint=1", headers[path].Get("Tracestate"), path)
	}
	assert.Empty(t, headers["/runner/jobs/heartbeat"].Get("Traceparent"))
}
