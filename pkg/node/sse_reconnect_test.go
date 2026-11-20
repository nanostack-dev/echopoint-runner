package node_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/httpcall"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// cut ends a connection after its frames.
type cut int

const (
	// cutNone ends the stream cleanly.
	cutNone cut = iota
	// cutShort closes the connection before the declared Content-Length:
	// the client reads an unexpected EOF.
	cutShort
	// cutReset closes the connection with a TCP RST: "connection reset by peer".
	cutReset
)

type sseConn struct {
	frames []string
	cut    cut
}

// scriptedSseServer answers connection i with conns[i], or with the last one
// once the script runs out, and records each connection's request headers.
func scriptedSseServer(t *testing.T, conns ...sseConn) (*httptest.Server, func() []http.Header) {
	t.Helper()
	var mu sync.Mutex
	var seen []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hit := len(seen)
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		conn := conns[min(hit, len(conns)-1)]

		w.Header().Set("Content-Type", "text/event-stream")
		if conn.cut == cutShort {
			w.Header().Set("Content-Length", "100000")
		}
		w.WriteHeader(http.StatusOK)
		for _, frame := range conn.frames {
			_, _ = fmt.Fprint(w, frame)
		}
		_ = http.NewResponseController(w).Flush()
		if conn.cut != cutReset {
			return
		}
		time.Sleep(50 * time.Millisecond) // let the client read the frames first
		raw, _, err := http.NewResponseController(w).Hijack()
		if !assert.NoError(t, err) {
			return
		}
		if tcp, ok := raw.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = raw.Close()
	}))
	t.Cleanup(srv.Close)
	return srv, func() []http.Header {
		mu.Lock()
		defer mu.Unlock()
		return append([]http.Header(nil), seen...)
	}
}

func idFrame(id string, payload any) string {
	return "id: " + id + "\n" + dataFrame(payload)
}

func executeSse(t *testing.T, n *node.SseNode) (*node.SseExecutionResult, *httpcall.Collector, error) {
	t.Helper()
	collector := httpcall.NewCollector()
	ctx := httpcall.WithCollector(context.Background(), collector)
	res, err := n.Execute(spi.ExecutionContext{Ctx: ctx, Inputs: map[string]any{}})
	return spi.MustAs[*node.SseExecutionResult](res), collector, err
}

func seq(n int) map[string]any { return map[string]any{"seq": n} }

func seqs(ns ...int) []any {
	out := make([]any, len(ns))
	for i, n := range ns {
		out[i] = map[string]any{"seq": float64(n)}
	}
	return out
}

// Echopoint's execution stream sends no ids and replays every event on each
// connect: the reconnect restarts collection instead of counting them twice.
func TestSseNode_RestartsAStreamWithoutIDsAfterACut(t *testing.T) {
	for name, kind := range map[string]cut{"unexpected EOF": cutShort, "connection reset": cutReset} {
		t.Run(name, func(t *testing.T) {
			srv, headers := scriptedSseServer(t,
				sseConn{frames: []string{dataFrame(seq(1)), dataFrame(seq(2))}, cut: kind},
				sseConn{frames: []string{dataFrame(seq(1)), dataFrame(seq(2)), dataFrame(seq(3))}},
			)
			n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 5000, MaxEvents: 3},
				mkAssertion(t, "json_path", "$.seq", "greater_than", "0"),
			)

			res, collector, err := executeSse(t, n)

			require.NoError(t, err)
			assert.Equal(t, seqs(1, 2, 3), res.Events)
			assert.Equal(t, "max_events", res.StopReason)
			assert.Len(t, res.AssertionResults, 3, "the discarded connection's results go too")
			require.Len(t, headers(), 2)
			assert.Empty(t, headers()[1].Get("Last-Event-ID"))
			require.Len(t, collector.Groups(), 1)
			assert.Equal(t, 2, collector.Groups()[0].Count(), "each connection is its own HTTP call")
		})
	}
}

func TestSseNode_ResumesAStreamWithIDsFromTheLastEventID(t *testing.T) {
	first := sseConn{frames: []string{idFrame("1", seq(1)), idFrame("2", seq(2))}, cut: cutShort}
	cases := map[string]sseConn{
		"server honours Last-Event-ID": {frames: []string{idFrame("3", seq(3))}},
		"server replays from the start": {frames: []string{
			idFrame("1", seq(1)), idFrame("2", seq(2)), idFrame("3", seq(3)),
		}},
	}
	for name, second := range cases {
		t.Run(name, func(t *testing.T) {
			srv, headers := scriptedSseServer(t, first, second)
			n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 5000, MaxEvents: 3})

			res, _, err := executeSse(t, n)

			require.NoError(t, err)
			assert.Equal(t, seqs(1, 2, 3), res.Events)
			assert.Equal(t, "max_events", res.StopReason)
			require.Len(t, headers(), 2)
			assert.Empty(t, headers()[0].Get("Last-Event-ID"))
			assert.Equal(t, "2", headers()[1].Get("Last-Event-ID"))
		})
	}
}

func TestSseNode_StopsReconnectingAfterThreeConnections(t *testing.T) {
	srv, headers := scriptedSseServer(t, sseConn{frames: []string{dataFrame(seq(1))}, cut: cutShort})
	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 5000, MaxEvents: 5})

	res, _, err := executeSse(t, n)

	userErr, ok := spi.AsUserError(err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, "SSE stream read error", userErr.Message)
	assert.Len(t, headers(), 3)
	assert.Equal(t, seqs(1), res.Events, "the last connection's events")
}

func TestSseNode_DoesNotReconnectWhenItShouldNot(t *testing.T) {
	stop := false
	cases := map[string]struct {
		data       node.SseData
		assertions []node.CompositeAssertion
	}{
		"a POST stream may have started work on the server": {
			data: node.SseData{Method: http.MethodPost},
		},
		"a restart would erase a survived assertion failure": {
			data:       node.SseData{StopOnAssertionFailure: &stop},
			assertions: []node.CompositeAssertion{mkAssertion(t, "json_path", "$.seq", "greater_than", "1")},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv, headers := scriptedSseServer(t,
				sseConn{frames: []string{dataFrame(seq(1))}, cut: cutShort},
				sseConn{frames: []string{dataFrame(seq(1)), dataFrame(seq(2))}},
			)
			data := tc.data
			data.TimeoutMs, data.MaxEvents = 5000, 2
			n := sseNode(t, srv.URL, data, tc.assertions...)

			_, _, err := executeSse(t, n)

			require.Error(t, err)
			assert.Len(t, headers(), 1)
		})
	}
}

func TestSseNode_DoesNotReconnectAfterACleanEnd(t *testing.T) {
	srv, headers := scriptedSseServer(t, sseConn{frames: []string{dataFrame(seq(1))}, cut: cutNone})
	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 5000, MaxEvents: 5})

	res, _, err := executeSse(t, n)

	require.NoError(t, err)
	assert.Equal(t, "eof", res.StopReason)
	assert.Len(t, headers(), 1)
}

func TestSseNode_ReconnectsWhenTheFirstConnectionIsReset(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		first := hits == 1
		mu.Unlock()
		if first {
			raw, _, err := http.NewResponseController(w).Hijack()
			if !assert.NoError(t, err) {
				return
			}
			if tcp, ok := raw.(*net.TCPConn); ok {
				_ = tcp.SetLinger(0)
			}
			_ = raw.Close()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, dataFrame(seq(1)))
	}))
	t.Cleanup(srv.Close)

	res, _, err := executeSse(t, sseNode(t, srv.URL, node.SseData{TimeoutMs: 5000}))

	require.NoError(t, err)
	assert.Equal(t, seqs(1), res.Events)
	assert.Equal(t, 2, hits)
}

// An id is committed only when its event is dispatched: a cut inside an event,
// or inside its id line, must not move Last-Event-ID past the last full event.
func TestSseNode_ResumesFromTheLastDispatchedEvent(t *testing.T) {
	cases := map[string]string{
		"cut inside the event data": "id: 2\ndata: {\"se",
		"cut inside the id line":    "id: 2",
	}
	for name, tail := range cases {
		t.Run(name, func(t *testing.T) {
			srv, headers := scriptedSseServer(t,
				sseConn{frames: []string{idFrame("1", seq(1)), tail}, cut: cutShort},
				sseConn{frames: []string{idFrame("2", seq(2)), idFrame("3", seq(3))}},
			)
			n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 5000, MaxEvents: 3})

			res, _, err := executeSse(t, n)

			require.NoError(t, err)
			assert.Equal(t, seqs(1, 2, 3), res.Events)
			require.Len(t, headers(), 2)
			assert.Equal(t, "1", headers()[1].Get("Last-Event-ID"))
		})
	}
}

// A collected event without its own id cannot be recognised in a replay, so
// the stream restarts instead of resuming.
func TestSseNode_RestartsWhenACollectedEventHadNoID(t *testing.T) {
	srv, headers := scriptedSseServer(t,
		sseConn{frames: []string{idFrame("1", seq(1)), dataFrame(seq(2))}, cut: cutShort},
		sseConn{frames: []string{idFrame("1", seq(1)), dataFrame(seq(2)), dataFrame(seq(3))}},
	)
	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 5000, MaxEvents: 3})

	res, _, err := executeSse(t, n)

	require.NoError(t, err)
	assert.Equal(t, seqs(1, 2, 3), res.Events)
	require.Len(t, headers(), 2)
	assert.Empty(t, headers()[1].Get("Last-Event-ID"))
}

// A deadline that elapses during the backoff keeps the transport failure: no
// stream was read, so it is not a clean timeout.
func TestSseNode_DeadlineDuringBackoffKeepsTheFailure(t *testing.T) {
	t.Run("connect refused", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := listener.Addr().String()
		require.NoError(t, listener.Close())

		res, collector, err := executeSse(t, sseNode(t, "http://"+addr+"/stream", node.SseData{TimeoutMs: 250}))

		userErr, ok := spi.AsUserError(err)
		require.True(t, ok, "got %v", err)
		assert.Equal(t, "CONNECTION_REFUSED", userErr.Code)
		assert.Nil(t, res.Events)
		require.Len(t, collector.Groups(), 1)
		assert.Equal(t, 2, collector.Groups()[0].Count())
	})
	t.Run("stream cut", func(t *testing.T) {
		srv, headers := scriptedSseServer(t, sseConn{frames: []string{dataFrame(seq(1))}, cut: cutShort})

		res, _, err := executeSse(t, sseNode(t, srv.URL, node.SseData{TimeoutMs: 80}))

		userErr, ok := spi.AsUserError(err)
		require.True(t, ok, "got %v", err)
		assert.Equal(t, "SSE stream read error", userErr.Message)
		assert.Equal(t, "eof", res.StopReason)
		assert.Len(t, headers(), 1)
	})
}
