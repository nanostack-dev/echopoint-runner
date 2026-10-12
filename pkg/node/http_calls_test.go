package node_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
