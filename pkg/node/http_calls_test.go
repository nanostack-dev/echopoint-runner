package node_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/httpcall"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

func TestRecordingClient_FinishesTheCallAtEndOfBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()
	collector := httpcall.NewCollector()
	ctx := httpcall.WithCollector(context.Background(), collector)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/x", nil)
	require.NoError(t, err)

	resp, err := node.RecordingClientForTest("get", "{{base}}/x").Do(req)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)

	groups := collector.Groups()
	require.Len(t, groups, 1, "the call is recorded at EOF, before the body is closed")
	assert.Equal(t, []int64{5}, groups[0].ResponseBytes)

	require.NoError(t, resp.Body.Close())
	assert.Equal(t, 1, collector.Groups()[0].Count(), "closing after EOF does not record it twice")
}

func TestPacingCancelledBeforeSendDoesNotRecordACall(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	collector := httpcall.NewCollector()
	ctx := httpcall.WithPacer(context.Background(), httpcall.NewPacer(time.Hour))
	require.NoError(t, httpcall.Wait(ctx))
	ctx = httpcall.WithCollector(ctx, collector)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	resp, err := node.RecordingClientForTest("get", server.URL).Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Zero(t, hits.Load())
	assert.Empty(t, collector.Groups())
}
