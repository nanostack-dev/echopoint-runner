package node_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/httpcall"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// resettingServer resets the connection of the first resets requests after
// reading them, then answers 200.
func resettingServer(t *testing.T, resets int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) > resets {
			w.WriteHeader(http.StatusOK)
			return
		}
		conn, _, err := http.NewResponseController(w).Hijack()
		if !assert.NoError(t, err) {
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0) // close with RST: "connection reset by peer"
		}
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// runRequest executes a request node and returns the HTTP calls it recorded.
func runRequest(t *testing.T, method, url string, headers map[string]string) (*httpcall.Collector, error) {
	t.Helper()
	reqNode := &node.RequestNode{
		BaseNode: node.BaseNode{ID: "call", NodeType: spi.KindRequest},
		Data: node.RequestData{
			Method: method, URL: url, Headers: headers, Body: map[string]any{"a": 1}, Timeout: 5000,
		},
	}
	collector := httpcall.NewCollector()
	ctx := httpcall.WithCollector(context.Background(), collector)
	_, err := reqNode.Execute(spi.ExecutionContext{Ctx: ctx})
	return collector, err
}

func TestRequestNode_RetriesAnIdempotentRequestAfterAReset(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			server, hits := resettingServer(t, 2)

			collector, err := runRequest(t, method, server.URL+"/items", nil)

			require.NoError(t, err)
			assert.Equal(t, int32(3), hits.Load())
			require.Len(t, collector.Groups(), 1)
			assert.Equal(t, 3, collector.Groups()[0].Count(), "every attempt is its own HTTP call")
		})
	}
}

func TestRequestNode_StopsAfterThreeAttempts(t *testing.T) {
	server, hits := resettingServer(t, 10)

	_, err := runRequest(t, http.MethodGet, server.URL+"/items", nil)

	userErr, ok := spi.AsUserError(err)
	require.True(t, ok)
	assert.Equal(t, "CONNECTION_FAILED", userErr.Code)
	assert.Equal(t, int32(3), hits.Load())
}

func TestRequestNode_DoesNotResendAPostAfterAReset(t *testing.T) {
	server, hits := resettingServer(t, 1)

	_, err := runRequest(t, http.MethodPost, server.URL+"/items", nil)

	require.Error(t, err, "the server may have created the resource before the reset")
	assert.Equal(t, int32(1), hits.Load())
}

func TestRequestNode_RetriesAPostCarryingAnIdempotencyKey(t *testing.T) {
	server, hits := resettingServer(t, 1)

	_, err := runRequest(t, http.MethodPost, server.URL+"/items", map[string]string{"Idempotency-Key": "k1"})

	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load())
}

func TestRequestNode_RetriesAPostWhoseConnectionWasRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	collector, err := runRequest(t, http.MethodPost, "http://"+addr+"/items", nil)

	userErr, ok := spi.AsUserError(err)
	require.True(t, ok)
	assert.Equal(t, "CONNECTION_REFUSED", userErr.Code)
	require.Len(t, collector.Groups(), 1)
	assert.Equal(t, 3, collector.Groups()[0].Count(), "a refused connection carried no request")
}
