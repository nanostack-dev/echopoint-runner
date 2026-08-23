package cloudworker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/cloudworker"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type launcherSpy struct {
	mu       sync.Mutex
	started  int
	jobIDs   []uuid.UUID
	tokens   []string
	release  chan struct{}
	returned error
}

func newLauncherSpy() *launcherSpy {
	return &launcherSpy{release: make(chan struct{})}
}

func (l *launcherSpy) launch(ctx context.Context, jobID uuid.UUID, jobToken string) error {
	l.mu.Lock()
	l.started++
	l.jobIDs = append(l.jobIDs, jobID)
	l.tokens = append(l.tokens, jobToken)
	l.mu.Unlock()
	select {
	case <-l.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return l.returned
}

func (l *launcherSpy) startCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.started
}

func (l *launcherSpy) waitForStarts(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if l.startCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Equalf(t, want, l.startCount(), "launcher never started %d jobs", want)
}

func assign(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/assign", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func assignJob(t *testing.T, handler http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	jobID := uuid.Must(uuid.NewV7())
	body := `{"job_id":"` + jobID.String() + `","job_token":"` + token + `"}`
	return assign(t, handler, body)
}

func status(t *testing.T, handler http.Handler) cloudworker.StatusResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)
	var body cloudworker.StatusResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func waitForStatus(t *testing.T, handler http.Handler, busy bool) cloudworker.StatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last cloudworker.StatusResponse
	for time.Now().Before(deadline) {
		last = status(t, handler)
		if last.Busy == busy {
			return last
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Equalf(t, busy, last.Busy, "status never reached busy=%v", busy)
	return last
}

func TestAssignStartsOneJobAndReportsItBusy(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	handler := cloudworker.NewServer(spy.launch, 1).Handler()
	jobID := uuid.Must(uuid.NewV7())

	recorder := assign(t, handler, `{"job_id":"`+jobID.String()+`","job_token":"raw-token"}`)
	require.Equal(t, http.StatusAccepted, recorder.Code)

	current := waitForStatus(t, handler, true)
	assert.Equal(t, []string{jobID.String()}, current.JobIDs)
	assert.Equal(t, 1, current.Running)
	assert.Equal(t, 1, current.Capacity)
	spy.waitForStarts(t, 1)
	assert.Equal(t, []string{"raw-token"}, spy.tokens)

	close(spy.release)
	idle := waitForStatus(t, handler, false)
	assert.Empty(t, idle.JobIDs)
	assert.Equal(t, 0, idle.Running)
}

func TestAssignAboveCapacityIsRejected(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	handler := cloudworker.NewServer(spy.launch, 3).Handler()

	for _, token := range []string{"first", "second", "third"} {
		require.Equal(t, http.StatusAccepted, assignJob(t, handler, token).Code)
	}
	spy.waitForStarts(t, 3)

	full := status(t, handler)
	assert.Equal(t, 3, full.Running)
	assert.Equal(t, 3, full.Capacity)
	assert.True(t, full.Busy)

	assert.Equal(t, http.StatusTooManyRequests, assignJob(t, handler, "fourth").Code)
	assert.Equal(t, 3, spy.startCount())

	close(spy.release)
	waitForStatus(t, handler, false)
}

func TestAssignRunsJobsConcurrently(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	handler := cloudworker.NewServer(spy.launch, 4).Handler()

	for _, token := range []string{"a", "b", "c"} {
		require.Equal(t, http.StatusAccepted, assignJob(t, handler, token).Code)
	}

	spy.waitForStarts(t, 3)
	running := status(t, handler)
	assert.Equal(t, 3, running.Running)
	assert.Len(t, running.JobIDs, 3)

	close(spy.release)
	waitForStatus(t, handler, false)
}

func TestAssignRejectsAJobItAlreadyHolds(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	handler := cloudworker.NewServer(spy.launch, 2).Handler()
	jobID := uuid.Must(uuid.NewV7())
	body := `{"job_id":"` + jobID.String() + `","job_token":"only-once"}`

	require.Equal(t, http.StatusAccepted, assign(t, handler, body).Code)
	spy.waitForStarts(t, 1)

	assert.Equal(t, http.StatusTooManyRequests, assign(t, handler, body).Code)
	assert.Equal(t, 1, spy.startCount())
	assert.Equal(t, 1, status(t, handler).Running)

	close(spy.release)
	waitForStatus(t, handler, false)
}

func TestAssignAcceptsAnotherJobAfterTheFirstFinishes(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	handler := cloudworker.NewServer(spy.launch, 1).Handler()

	require.Equal(
		t,
		http.StatusAccepted,
		assign(t, handler, `{"job_id":"`+uuid.Must(uuid.NewV7()).String()+`","job_token":"first"}`).Code,
	)
	spy.waitForStarts(t, 1)
	close(spy.release)
	waitForStatus(t, handler, false)

	second := uuid.Must(uuid.NewV7())
	require.Equal(
		t,
		http.StatusAccepted,
		assign(t, handler, `{"job_id":"`+second.String()+`","job_token":"second"}`).Code,
	)
	spy.waitForStarts(t, 2)
	assert.Equal(t, []string{"first", "second"}, spy.tokens)
}

func TestAssignRejectsMalformedBody(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	handler := cloudworker.NewServer(spy.launch, 2).Handler()

	cases := map[string]string{
		"not json":          `{`,
		"missing token":     `{"job_id":"` + uuid.Must(uuid.NewV7()).String() + `"}`,
		"missing job id":    `{"job_token":"raw-token"}`,
		"job id not a uuid": `{"job_id":"not-a-uuid","job_token":"raw-token"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, http.StatusBadRequest, assign(t, handler, body).Code)
		})
	}
	assert.Equal(t, 0, spy.startCount())
	assert.False(t, status(t, handler).Busy)
}

func TestCapacityBelowOneStillRunsOneJob(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	handler := cloudworker.NewServer(spy.launch, 0).Handler()

	require.Equal(t, http.StatusAccepted, assignJob(t, handler, "only").Code)
	spy.waitForStarts(t, 1)
	assert.Equal(t, 1, status(t, handler).Capacity)

	assert.Equal(t, http.StatusTooManyRequests, assignJob(t, handler, "second").Code)

	close(spy.release)
	waitForStatus(t, handler, false)
}

func TestHealthIsAlwaysServed(t *testing.T) {
	t.Parallel()
	handler := cloudworker.NewServer(newLauncherSpy().launch, 1).Handler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStopAcceptingRefusesNewJobsAndDrainsRunningOnes(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	server := cloudworker.NewServer(spy.launch, 2)
	handler := server.Handler()

	require.Equal(t, http.StatusAccepted, assignJob(t, handler, "in-flight").Code)
	spy.waitForStarts(t, 1)

	server.StopAccepting()

	assert.Equal(t, http.StatusTooManyRequests, assignJob(t, handler, "too-late").Code)
	assert.Equal(t, 1, spy.startCount())
	assert.Equal(t, 1, server.Running(), "a job already running must survive the drain")

	close(spy.release)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, server.WaitIdle(ctx))
	assert.Equal(t, 0, server.Running())
}

func TestWaitIdleReturnsAtOnceWhenNoJobRuns(t *testing.T) {
	t.Parallel()
	server := cloudworker.NewServer(newLauncherSpy().launch, 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, server.WaitIdle(ctx))
}

func TestWaitIdleGivesUpWhenAJobOutlastsTheDeadline(t *testing.T) {
	t.Parallel()
	spy := newLauncherSpy()
	server := cloudworker.NewServer(spy.launch, 1)
	handler := server.Handler()

	require.Equal(t, http.StatusAccepted, assignJob(t, handler, "slow").Code)
	spy.waitForStarts(t, 1)
	server.StopAccepting()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, server.WaitIdle(ctx), context.DeadlineExceeded)

	close(spy.release)
}
