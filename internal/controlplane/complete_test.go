package controlplane_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completionServer answers each complete attempt with the status statusFor
// returns, given the attempt number (from 1) and the time since the first one.
type completionServer struct {
	mu       sync.Mutex
	first    time.Time
	bodies   []string
	statusOf func(attempt int, sinceFirst time.Duration) int
}

func (s *completionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	if s.first.IsZero() {
		s.first = time.Now()
	}
	s.bodies = append(s.bodies, string(body))
	status := s.statusOf(len(s.bodies), time.Since(s.first))
	s.mu.Unlock()
	w.WriteHeader(status)
}

func (s *completionServer) attempts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// completeAgainst completes with the production backoff and a minute of budget.
func completeAgainst(
	t *testing.T,
	statusOf func(attempt int, sinceFirst time.Duration) int,
) (*completionServer, error) {
	t.Helper()
	return completeWithBackoff(t, time.Minute, controlplane.Backoff{}, statusOf)
}

func completeWithBackoff(
	t *testing.T,
	budget time.Duration,
	backoff controlplane.Backoff,
	statusOf func(attempt int, sinceFirst time.Duration) int,
) (*completionServer, error) {
	t.Helper()
	server := &completionServer{statusOf: statusOf}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	client := controlplane.NewRunnerClient(controlplane.Config{BaseURL: httpServer.URL, RequestTimeout: time.Second})
	controlplane.SetBackoff(client, backoff)
	err := client.Complete(ctx, uuid.New(), controlplane.CompleteJobRequest{RunnerID: "runner", Status: "completed"})
	return server, err
}

// A control plane redeploy answers 503 for a few seconds; the completion must
// outlast it rather than turn a passed run into an expired lease.
func TestCompleteRetriesThroughATwoSecondRunOf503s(t *testing.T) {
	server, err := completeAgainst(t, func(_ int, sinceFirst time.Duration) int {
		if sinceFirst < 2*time.Second {
			return http.StatusServiceUnavailable
		}
		return http.StatusNoContent
	})

	require.NoError(t, err)
	assert.Greater(t, len(server.attempts()), 3)
}

func TestCompleteRetriesARateLimit(t *testing.T) {
	server, err := completeAgainst(t, func(attempt int, _ time.Duration) int {
		if attempt == 1 {
			return http.StatusTooManyRequests
		}
		return http.StatusNoContent
	})

	require.NoError(t, err)
	assert.Len(t, server.attempts(), 2)
}

// A 409 means the job is no longer this runner's to complete: retrying cannot help.
func TestCompleteStopsAtOnceOnARejection(t *testing.T) {
	server, err := completeAgainst(t, func(int, time.Duration) int {
		return http.StatusConflict
	})

	statusErr, ok := errors.AsType[*controlplane.StatusError](err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, http.StatusConflict, statusErr.StatusCode)
	assert.Len(t, server.attempts(), 1)
}

func TestCompleteGivesUpWhenItsBudgetEnds(t *testing.T) {
	started := time.Now()
	fast := controlplane.Backoff{Initial: 10 * time.Millisecond, Max: 40 * time.Millisecond}
	server, err := completeWithBackoff(t, 300*time.Millisecond, fast, func(int, time.Duration) int {
		return http.StatusServiceUnavailable
	})

	statusErr, ok := errors.AsType[*controlplane.StatusError](err)
	require.True(t, ok, "the last answer is returned, got %v", err)
	assert.Equal(t, http.StatusServiceUnavailable, statusErr.StatusCode)
	assert.Less(t, time.Since(started), 2*time.Second)
	assert.Greater(t, len(server.attempts()), 3)
}

// The control plane's job-token path recognises a repeated completion by its
// digest, so every attempt must send the same bytes. The runner-API-key path
// does not replay yet and answers a repeat with 409.
func TestCompleteSendsTheSameBodyOnEveryAttempt(t *testing.T) {
	server, err := completeAgainst(t, func(attempt int, _ time.Duration) int {
		if attempt < 3 {
			return http.StatusBadGateway
		}
		return http.StatusNoContent
	})

	require.NoError(t, err)
	attempts := server.attempts()
	require.Len(t, attempts, 3)
	assert.Equal(t, attempts[0], attempts[1])
	assert.Equal(t, attempts[0], attempts[2])
}

// completeAgainstNotFound completes against a server that always answers 404
// with body, and returns the number of attempts.
func completeAgainstNotFound(t *testing.T, body string) (int, error) {
	t.Helper()
	var (
		mu       sync.Mutex
		attempts int
	)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(httpServer.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := controlplane.NewRunnerClient(controlplane.Config{BaseURL: httpServer.URL, RequestTimeout: time.Second})
	controlplane.SetBackoff(client, controlplane.Backoff{Initial: 10 * time.Millisecond, Max: 40 * time.Millisecond})
	err := client.Complete(ctx, uuid.New(), controlplane.CompleteJobRequest{RunnerID: "runner", Status: "completed"})
	mu.Lock()
	defer mu.Unlock()
	return attempts, err
}

// A 404 with an API error code is the control plane saying the job is gone,
// for example after a workspace delete during the run. Retrying would hold the
// runner slot for the whole completion budget.
func TestCompleteStopsAtOnceWhenTheAPISaysTheJobIsGone(t *testing.T) {
	attempts, err := completeAgainstNotFound(t,
		`{"errors":[{"code":"RUNNER_JOB_NOT_FOUND","message":"runner job does not exist"}]}`)

	statusErr, ok := errors.AsType[*controlplane.StatusError](err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, "RUNNER_JOB_NOT_FOUND", statusErr.Code)
	assert.Equal(t, 1, attempts)
}

// A 404 without an API error code is the edge answering while it reroutes
// during a control plane redeploy, so it is retried.
func TestCompleteRetriesAnEdgeNotFound(t *testing.T) {
	attempts, err := completeAgainstNotFound(t, "404 page not found")

	statusErr, ok := errors.AsType[*controlplane.StatusError](err)
	require.True(t, ok, "got %v", err)
	assert.Empty(t, statusErr.Code)
	assert.Greater(t, attempts, 3)
}
