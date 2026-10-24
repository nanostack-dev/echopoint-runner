package jobrunner_test

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
	"github.com/nanostack-dev/echopoint-runner/pkg/jobrunner"
)

func TestRunReportsOneClaimedJobWithItsToken(t *testing.T) {
	jobID := uuid.New()
	state := &reportState{t: t, jobID: jobID}
	server := httptest.NewServer(state)
	defer server.Close()

	config := jobrunner.Config{
		BaseURL: server.URL, JobToken: "one-job-token", RunnerID: "ephemeral-test",
		BootID: uuid.New(), Heartbeat: 5 * time.Millisecond,
	}
	flowDefinition := json.RawMessage(
		`{"version":"1.0","name":"t","nodes":[` +
			`{"id":"wait","type":"delay","data":{"duration":70}}],"edges":[]}`,
	)
	job := jobrunner.Job{
		JobID:          jobID,
		ExecutionID:    uuid.New(),
		FlowID:         uuid.New(),
		FlowDefinition: flowDefinition,
	}
	client, err := jobrunner.NewClient(config)
	if err != nil {
		t.Fatalf("create Job client: %v", err)
	}
	result, err := client.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("run one Job: %v", err)
	}
	if result.Status != "completed" || result.Execution == nil || !result.Execution.Success {
		t.Fatalf("unexpected result: %+v", result)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.events == 0 || state.heartbeats == 0 || state.completions != 2 {
		t.Fatalf("reports: events=%d heartbeats=%d completions=%d", state.events, state.heartbeats, state.completions)
	}
}

type reportState struct {
	mu          sync.Mutex
	t           *testing.T
	jobID       uuid.UUID
	events      int
	heartbeats  int
	completions int
}

func (s *reportState) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Job-Token") != "one-job-token" || r.Header.Get("X-Api-Key") != "" {
		s.t.Errorf("wrong Job credential headers: %v", r.Header)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/events"):
		var body struct {
			Events []struct {
				Sequence int64 `json:"sequence"`
			} `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Error(err)
		}
		s.events += len(body.Events)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int64{
			"last_accepted_sequence": body.Events[len(body.Events)-1].Sequence,
		})
	case r.URL.Path == "/runner/jobs/heartbeat":
		s.heartbeats++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"job_id": s.jobID, "status": "renewed"}})
	case strings.HasSuffix(r.URL.Path, "/complete"):
		s.completions++
		if s.completions == 1 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		s.t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

// The CLI copies the ephemeral claim's trace_context into Job.TraceContext. The
// Job's events and complete calls carry it; the heartbeat covers no single trace.
func TestRunSendsTheJobTraceContextOnItsCallbacks(t *testing.T) {
	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	jobID := uuid.New()
	var mu sync.Mutex
	traceparents := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		mu.Lock()
		traceparents[kind] = append(traceparents[kind], r.Header.Get("Traceparent")+"|"+r.Header.Get("Tracestate"))
		mu.Unlock()
		switch kind {
		case "complete":
			w.WriteHeader(http.StatusNoContent)
		case "heartbeat":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"job_id": jobID, "status": "renewed"}})
		default:
			_, _ = w.Write([]byte(`{"last_accepted_sequence":100}`))
		}
	}))
	defer server.Close()

	var job jobrunner.Job
	if err := json.Unmarshal([]byte(`{"trace_context":{"traceparent":"`+traceparent+
		`","tracestate":"echopoint=1"}}`), &job); err != nil {
		t.Fatal(err)
	}
	job.JobID = jobID
	job.ExecutionID = uuid.New()
	job.FlowDefinition = json.RawMessage(
		`{"version":"1.0","name":"t","nodes":[{"id":"wait","type":"delay","data":{"duration":40}}],"edges":[]}`,
	)
	client, err := jobrunner.NewClient(jobrunner.Config{
		BaseURL: server.URL, JobToken: "one-job-token", RunnerID: "ephemeral-test",
		BootID: uuid.New(), Heartbeat: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Run(context.Background(), job); err != nil {
		t.Fatalf("run one Job: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := traceparent + "|echopoint=1"
	for _, kind := range []string{"events", "complete"} {
		if len(traceparents[kind]) == 0 {
			t.Fatalf("no %s call", kind)
		}
		for _, got := range traceparents[kind] {
			if got != want {
				t.Errorf("%s trace headers = %q, want %q", kind, got, want)
			}
		}
	}
	for _, got := range traceparents["heartbeat"] {
		if got != "|" {
			t.Errorf("heartbeat carried trace headers %q", got)
		}
	}
}
