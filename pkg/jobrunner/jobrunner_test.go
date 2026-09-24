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
	result, err := jobrunner.Run(context.Background(), config, job)
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
