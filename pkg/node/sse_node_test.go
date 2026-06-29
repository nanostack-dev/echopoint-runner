package node_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseServer starts an httptest.Server that writes the given SSE frames (each a
// complete event block, e.g. "data: {...}\n\n") and flushes after every write so
// the client receives them incrementally. delay is slept before each frame.
func sseServer(t *testing.T, delay time.Duration, frames ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		for _, frame := range frames {
			if delay > 0 {
				time.Sleep(delay)
			}
			_, _ = fmt.Fprint(w, frame)
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dataFrame(payload any) string {
	encoded, _ := json.Marshal(payload)
	return fmt.Sprintf("data: %s\n\n", encoded)
}

func sseNode(t *testing.T, url string, data node.SseData, assertions ...node.CompositeAssertion) *node.SseNode {
	t.Helper()
	data.URL = url
	return &node.SseNode{
		BaseNode: node.BaseNode{
			ID:          "sse1",
			DisplayName: "Stream",
			NodeType:    node.TypeSse,
			Assertions:  assertions,
		},
		Data: data,
	}
}

func TestSseNode_DecodeViaUnmarshalNode(t *testing.T) {
	raw := `{
		"id":"sse1",
		"type":"sse",
		"data":{"url":"https://example.com/stream","max_events":5,"timeout_ms":2000,"completion_event":"done"}
	}`
	n, err := node.UnmarshalNode([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, node.TypeSse, n.GetType())
	require.Equal(t, node.RunWhenOnSuccess, n.GetRunWhen())

	sse, ok := node.AsSseNode(n)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/stream", sse.GetData().URL)
	assert.Equal(t, 5, sse.GetData().MaxEvents)
	assert.Equal(t, 2000, sse.GetData().TimeoutMs)
	assert.Equal(t, "done", sse.GetData().CompletionEvent)
	assert.Equal(t, []string{"events", "count", "last"}, sse.OutputSchema())
}

func TestSseNode_CollectsEventsWithPassingAssertion(t *testing.T) {
	srv := sseServer(t, 0,
		dataFrame(map[string]any{"seq": 1, "status": "ok"}),
		dataFrame(map[string]any{"seq": 2, "status": "ok"}),
		dataFrame(map[string]any{"seq": 3, "status": "ok"}),
	)

	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 2000},
		mkAssertion(t, "jsonPath", "$.status", "equals", "ok"),
	)

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	assert.Equal(t, 3, sseRes.EventCount)
	assert.Len(t, sseRes.Events, 3)
	// 3 events x 1 assertion each = 3 recorded assertion results.
	assert.Len(t, sseRes.AssertionResults, 3)
	for i, ar := range sseRes.AssertionResults {
		assert.True(t, ar.Passed, "assertion %d should pass", i)
		assert.Equal(t, i, ar.Index, "Index should carry the event index")
	}
	assert.Equal(t, "eof", sseRes.StopReason)

	outputs := sseRes.GetOutputs()
	assert.Equal(t, 3, outputs["count"])
	require.Contains(t, outputs, "last")
	last, ok := outputs["last"].(map[string]any)
	require.True(t, ok)
	lastSeq, ok := last["seq"].(float64)
	require.True(t, ok)
	assert.InDelta(t, float64(3), lastSeq, 0.0001)
}

func TestSseNode_FailingAssertionStopsAfterEvent(t *testing.T) {
	srv := sseServer(t, 0,
		dataFrame(map[string]any{"seq": 1, "status": "ok"}),
		dataFrame(map[string]any{"seq": 2, "status": "bad"}),
		dataFrame(map[string]any{"seq": 3, "status": "ok"}),
	)

	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 2000},
		mkAssertion(t, "jsonPath", "$.status", "equals", "ok"),
	)

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.Error(t, err)

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	// Stopped after event index 1 (the second event), so 2 events captured.
	assert.Equal(t, 2, sseRes.EventCount)
	assert.Equal(t, "assertion_failure", sseRes.StopReason)
	require.NotNil(t, sseRes.ErrorCode)
	assert.Equal(t, "SSE_FAILED", *sseRes.ErrorCode)
	// Last recorded assertion is the failing one, tagged with event index 1.
	last := sseRes.AssertionResults[len(sseRes.AssertionResults)-1]
	assert.False(t, last.Passed)
	assert.Equal(t, 1, last.Index)
}

func TestSseNode_FailingAssertionContinuesWhenStopDisabled(t *testing.T) {
	srv := sseServer(t, 0,
		dataFrame(map[string]any{"status": "ok"}),
		dataFrame(map[string]any{"status": "bad"}),
		dataFrame(map[string]any{"status": "ok"}),
	)

	stop := false
	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 2000, StopOnAssertionFailure: &stop},
		mkAssertion(t, "jsonPath", "$.status", "equals", "ok"),
	)

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	assert.Equal(t, 3, sseRes.EventCount)
	assert.Equal(t, "eof", sseRes.StopReason)
	assert.False(t, sseRes.AssertionResults[1].Passed)
}

func TestSseNode_MaxEventsStopsEarly(t *testing.T) {
	srv := sseServer(t, 0,
		dataFrame(map[string]any{"seq": 1}),
		dataFrame(map[string]any{"seq": 2}),
		dataFrame(map[string]any{"seq": 3}),
		dataFrame(map[string]any{"seq": 4}),
	)

	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 2000, MaxEvents: 2})

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	assert.Equal(t, 2, sseRes.EventCount)
	assert.Equal(t, "max_events", sseRes.StopReason)
}

func TestSseNode_CompletionEventStopsStream(t *testing.T) {
	srv := sseServer(t, 0,
		dataFrame(map[string]any{"seq": 1}),
		"event: done\ndata: {\"final\":true}\n\n",
		dataFrame(map[string]any{"seq": 3}),
	)

	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 2000, CompletionEvent: "done"})

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	assert.Equal(t, 2, sseRes.EventCount)
	assert.Equal(t, "completion_event", sseRes.StopReason)
	last, ok := sseRes.Events[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, last["final"])
}

func TestSseNode_TimeoutReturnsOnDeadline(t *testing.T) {
	// Server sleeps 200ms before each frame; node timeout is 50ms, so it should
	// return on the deadline with whatever it has (likely zero events).
	srv := sseServer(t, 200*time.Millisecond,
		dataFrame(map[string]any{"seq": 1}),
		dataFrame(map[string]any{"seq": 2}),
	)

	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 50})

	start := time.Now()
	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 2*time.Second, "node must return promptly on deadline")

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	assert.Equal(t, "timeout", sseRes.StopReason)
}

func TestSseNode_NonOKStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 2000})

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.Error(t, err)

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	require.NotNil(t, sseRes.ErrorCode)
	assert.Equal(t, "SSE_FAILED", *sseRes.ErrorCode)
	assert.Contains(t, err.Error(), "503")
}

func TestSseNode_TemplatedURLAndHeaders(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, dataFrame(map[string]any{"ok": true}))
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	n := &node.SseNode{
		BaseNode: node.BaseNode{ID: "sse1", DisplayName: "Stream", NodeType: node.TypeSse},
		Data: node.SseData{
			URL:       srv.URL + "/{{path}}",
			TimeoutMs: 2000,
			Headers:   map[string]string{"Authorization": "Bearer {{token}}"},
		},
	}

	// InputSchema should infer the template variables from URL and headers.
	assert.ElementsMatch(t, []string{"path", "token"}, n.InputSchema())

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{
		"path":  "events",
		"token": "secret",
	}})
	require.NoError(t, err)

	assert.Equal(t, "Bearer secret", gotAuth)
	assert.Equal(t, "text/event-stream", gotAccept)
	sseRes := node.MustAs[*node.SseExecutionResult](res)
	assert.Equal(t, 1, sseRes.EventCount)
}

func TestSseNode_NonJSONDataFallsBackToString(t *testing.T) {
	srv := sseServer(t, 0,
		"data: hello world\n\n",
		"data: plain text\n\n",
	)

	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 2000})

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	require.Equal(t, 2, sseRes.EventCount)
	assert.Equal(t, "hello world", sseRes.Events[0])
	assert.Equal(t, "plain text", sseRes.Events[1])
}

func TestSseNode_MultiLineDataAndComments(t *testing.T) {
	// Comment line (": keep-alive") is ignored; two data lines join with "\n".
	srv := sseServer(t, 0,
		": keep-alive\ndata: line1\ndata: line2\n\n",
	)

	n := sseNode(t, srv.URL, node.SseData{TimeoutMs: 2000})

	res, err := n.Execute(node.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)

	sseRes := node.MustAs[*node.SseExecutionResult](res)
	require.Equal(t, 1, sseRes.EventCount)
	assert.Equal(t, "line1\nline2", sseRes.Events[0])
}
