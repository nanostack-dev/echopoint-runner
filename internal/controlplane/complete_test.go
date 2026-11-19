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

// The control plane recognises a repeated completion by its digest, so every
// attempt must send the same bytes.
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
