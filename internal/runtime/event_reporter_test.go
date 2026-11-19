//nolint:testpackage // white-box: drives the unexported progress reporter
package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const controlPlaneMaxEventsPerRequest = 100

func TestJobEventReporter_ABacklogAfterAFailedFlushIsSentInAcceptedBatchSizes(t *testing.T) {
	var (
		mu         sync.Mutex
		failNext   = true
		batchSizes []int
		accepted   int64
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.SendJobEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		batchSizes = append(batchSizes, len(body.Events))
		if failNext {
			failNext = false
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if len(body.Events) > controlPlaneMaxEventsPerRequest {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		accepted = body.Events[len(body.Events)-1].Sequence
		_ = json.NewEncoder(w).Encode(controlplane.SendJobEventsResponse{LastAcceptedSequence: accepted})
	}))
	defer server.Close()

	reporter := &jobEventReporter{
		client:         controlplane.NewRunnerClient(controlplane.Config{BaseURL: server.URL}),
		jobID:          uuid.New(),
		requestTimeout: 5 * time.Second,
	}
	ctx := context.Background()
	require.NoError(t, reporter.enqueue("node.started", map[string]string{"n": "0"}))
	require.Error(t, reporter.Flush(ctx))
	for i := range 150 {
		reporter.mu.Lock()
		reporter.nextSequence++
		reporter.pending = append(reporter.pending, controlplane.RunnerProgressEvent{
			Sequence: reporter.nextSequence, Type: "node.started", Payload: map[string]int{"n": i},
		})
		reporter.mu.Unlock()
	}

	require.NoError(t, reporter.FlushWithRetry(ctx))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, int64(151), accepted, "every event reaches the control plane")
	for _, size := range batchSizes {
		assert.LessOrEqual(t, size, reporterMaxBatchEvents)
	}
}

func reporterAgainst(t *testing.T, statusOf func(sinceFirst time.Duration) int) (*jobEventReporter, *atomic.Int32) {
	t.Helper()
	var (
		mu    sync.Mutex
		first time.Time
		calls atomic.Int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.SendJobEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls.Add(1)
		mu.Lock()
		if first.IsZero() {
			first = time.Now()
		}
		status := statusOf(time.Since(first))
		mu.Unlock()
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(controlplane.SendJobEventsResponse{
			LastAcceptedSequence: body.Events[len(body.Events)-1].Sequence,
		})
	}))
	t.Cleanup(server.Close)

	reporter := &jobEventReporter{
		client: controlplane.NewRunnerClient(
			controlplane.Config{BaseURL: server.URL, RequestTimeout: time.Second},
		),
		jobID:          uuid.New(),
		requestTimeout: time.Second,
	}
	reporter.mu.Lock()
	reporter.nextSequence = 1
	reporter.pending = []controlplane.RunnerProgressEvent{
		{Sequence: 1, Type: "node.started", Payload: map[string]int{}},
	}
	reporter.mu.Unlock()
	return reporter, &calls
}

// The final flush precedes completion, which the control plane refuses while
// events are missing, so it must outlast a short control-plane outage too.
func TestFlushWithRetrySurvivesATwoSecondRunOf503s(t *testing.T) {
	reporter, _ := reporterAgainst(t, func(sinceFirst time.Duration) int {
		if sinceFirst < 2*time.Second {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	require.NoError(t, reporter.FlushWithRetry(ctx))
	assert.Equal(t, int64(1), *reporter.LastSequencePtr())
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	assert.Empty(t, reporter.pending)
}

// A sequence conflict will not clear on retry; it must not spend the budget.
func TestFlushWithRetryStopsAtOnceOnAConflict(t *testing.T) {
	reporter, calls := reporterAgainst(t, func(time.Duration) int { return http.StatusConflict })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	require.Error(t, reporter.FlushWithRetry(ctx))
	assert.Equal(t, int32(1), calls.Load())
}
