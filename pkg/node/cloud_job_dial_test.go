package node_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/require"
)

func TestCloudJobRequestDoesNotReachLoopback(t *testing.T) {
	t.Setenv("CLOUD_JOB_ID", "job-1")
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	t.Cleanup(server.Close)

	_, err := requestNode(server.URL).Execute(spi.ExecutionContext{})

	require.Error(t, err)
	require.False(t, hit)
}

func TestRequestNodeReachesLoopbackOutsideACloudJob(t *testing.T) {
	t.Setenv("CLOUD_JOB_ID", "")
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	_, err := requestNode(server.URL).Execute(spi.ExecutionContext{})

	require.NoError(t, err)
	require.True(t, hit)
}

func requestNode(url string) *node.RequestNode {
	return &node.RequestNode{
		BaseNode: node.BaseNode{ID: "step", DisplayName: "Call", NodeType: spi.KindRequest},
		Data: node.RequestData{
			Method:  http.MethodGet,
			URL:     url,
			Timeout: 1000,
		},
	}
}
