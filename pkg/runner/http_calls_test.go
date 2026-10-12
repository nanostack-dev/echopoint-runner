package runner_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/httpcall"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func statusServer(t *testing.T) *httptest.Server {
	t.Helper()
	var exportPolls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/export":
			state := "pending"
			if exportPolls.Add(1) >= 3 {
				state = "ready"
			}
			_, _ = fmt.Fprintf(w, `{"state":%q}`, state)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func parseFlow(t *testing.T, definition string, inputKeys ...string) flow.Flow {
	t.Helper()
	parsed, err := flow.ParseFromJSONWithOptions(
		[]byte(definition),
		flow.ParseOptions{AllowedInitialInputKeys: inputKeys},
	)
	require.NoError(t, err)
	return *parsed
}

func requestNode(id, url string) string {
	return fmt.Sprintf(`{"id":%q,"type":"request","data":{"method":"get","url":%q}}`, id, url)
}

func loopFlow(url string, items int) string {
	list := make([]string, items)
	for i := range items {
		list[i] = strconv.Itoa(i)
	}
	return `{"version":"1.0","name":"t","nodes":[{"id":"each","type":"loop","data":{
		"items":[` + strings.Join(list, ",") + `],
		"body":{"nodes":[` + requestNode("get", url) + `],"edges":[]}
	}}],"edges":[]}`
}

func httpCalls(t *testing.T, result *spi.FlowExecutionResult, nodeID string) []spi.HTTPCallGroup {
	t.Helper()
	nodeResult, ok := result.ExecutionResults[nodeID]
	require.True(t, ok, "no result for node %s", nodeID)
	// Read the wire shape: the result the caller gets is a masked copy whose
	// node results only keep their fields in JSON.
	encoded, err := json.Marshal(nodeResult)
	require.NoError(t, err)
	var decoded struct {
		HTTPCalls []spi.HTTPCallGroup `json:"http_calls"`
	}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return decoded.HTTPCalls
}

func TestRun_RequestNodeResultHoldsItsCallWithoutResolvedValues(t *testing.T) {
	server := statusServer(t)
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[`+
		requestNode("ok", "{{baseUrl}}/users/{{ userId }}?token={{secret}}")+`,`+
		requestNode("missing", "{{baseUrl}}/missing")+
		`],"edges":[{"id":"e","source":"ok","target":"missing"}]}`, "baseUrl", "userId", "secret")

	result, _ := runner.Run(flowDef, map[string]any{
		"baseUrl": server.URL, "userId": "42", "secret": "s3cr3t",
	}, runner.WithSecretInputKeys([]string{"secret"}))

	ok := httpCalls(t, result, "ok")
	require.Len(t, ok, 1)
	assert.Equal(t, "ok", ok[0].NodeID)
	assert.Equal(t, "GET", ok[0].Method)
	assert.Equal(t, server.Listener.Addr().String(), ok[0].Host)
	assert.Equal(t, "/users/{userId}", ok[0].PathTemplate)
	assert.Equal(t, []int64{0}, ok[0].OffsetsMs)
	assert.Equal(t, []int{http.StatusOK}, ok[0].StatusCodes)
	assert.Equal(t, []int64{int64(len(`{"ok":true}`))}, ok[0].ResponseBytes)
	assert.Empty(t, ok[0].TransportErrors)

	missing := httpCalls(t, result, "missing")
	require.Len(t, missing, 1)
	assert.Equal(t, []int{http.StatusNotFound}, missing[0].StatusCodes)
}

func TestRun_TransportFailureHasNoStatusAndAClass(t *testing.T) {
	server := statusServer(t)
	unreachable := server.URL
	server.Close()
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[`+requestNode("down", unreachable)+`],"edges":[]}`)

	result, err := runner.Run(flowDef, map[string]any{})

	require.Error(t, err)
	down := httpCalls(t, result, "down")
	require.Len(t, down, 1)
	assert.Equal(t, []int{0}, down[0].StatusCodes)
	assert.Equal(t, map[string]spi.HTTPCallErrorClass{"0": spi.HTTPCallErrorConnect}, down[0].TransportErrors)
}

func TestRun_LoopIterationsShareOneGroupOnTheLoopNode(t *testing.T) {
	server := statusServer(t)
	flowDef := parseFlow(t, loopFlow(server.URL+"/items/{{item}}", 3))

	result, err := runner.Run(flowDef, map[string]any{})

	require.NoError(t, err)
	groups := httpCalls(t, result, "each")
	require.Len(t, groups, 1)
	assert.Equal(t, "get", groups[0].NodeID, "the group names the body node that sent the calls")
	assert.Equal(t, "/items/{item}", groups[0].PathTemplate)
	assert.Equal(t, 3, groups[0].Count())
	assert.Equal(t, []int{200, 200, 200}, groups[0].StatusCodes)
	assert.Len(t, groups[0].DurationsMs, 3)
	assert.True(t, groups[0].OffsetsMs[0] == 0 && groups[0].OffsetsMs[2] >= groups[0].OffsetsMs[1])
}

func TestRun_PollAttemptsShareOneGroupOnThePollNode(t *testing.T) {
	server := statusServer(t)
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[{"id":"wait_export","type":"poll",
		"assertions":[{"extractor_type":"json_path","extractor_data":{"path":"$['check.state']"},
			"operator_type":"equals","operator_data":{"value":"ready"}}],
		"data":{"max_attempts":5,"interval_ms":1,"body":{"nodes":[
			{"id":"check","type":"request","data":{"method":"GET","url":"`+server.URL+`/export"},
			 "outputs":[{"name":"state","extractor":{"type":"json_path","path":"$.state"}}]}
		],"edges":[]}}}],"edges":[]}`)

	result, err := runner.Run(flowDef, map[string]any{})

	require.NoError(t, err)
	groups := httpCalls(t, result, "wait_export")
	require.Len(t, groups, 1)
	assert.Equal(t, "check", groups[0].NodeID)
	assert.Equal(t, 3, groups[0].Count(), "the poll stopped on the third attempt")
}

func TestRun_HTTPCallLimitCountsTheWholeExecution(t *testing.T) {
	server := statusServer(t)
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[`+
		requestNode("first", server.URL+"/a")+`,{"id":"each","type":"loop","data":{
		"items":[1,2,3],
		"body":{"nodes":[`+requestNode("get", server.URL+"/items/{{item}}")+`],"edges":[]}
	}}],"edges":[{"id":"e","source":"first","target":"each"}]}`)

	result, err := runner.Run(flowDef, map[string]any{}, runner.WithHTTPCallLimit(3))

	require.Error(t, err)
	userErr, ok := spi.AsUserError(err)
	require.True(t, ok, "expected a user error, got %v", err)
	assert.Equal(t, httpcall.LimitExceededCode, userErr.Code)
	encoded, encodeErr := json.Marshal(result.ExecutionResults["each"])
	require.NoError(t, encodeErr)
	assert.Contains(t, string(encoded), `"error_code":"HTTP_CALL_LIMIT_EXCEEDED"`)
	assert.Equal(t, 1, httpCalls(t, result, "first")[0].Count())
	assert.Equal(t, 2, httpCalls(t, result, "each")[0].Count(),
		"the third loop call would be the fourth of the execution, so it is refused and not recorded")
}

type finishedNodes struct {
	engine.NoopObserver

	mu      sync.Mutex
	results map[string]spi.AnyResult
}

func (o *finishedNodes) NodeFinished(evt engine.NodeFinishedEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.results[evt.NodeID] = evt.Result
}

func TestRun_ProgressEventCarriesTheNodeCalls(t *testing.T) {
	server := statusServer(t)
	flowDef := parseFlow(t, loopFlow(server.URL+"/items/{{item}}", 2))
	observer := &finishedNodes{results: map[string]spi.AnyResult{}}

	_, err := runner.Run(flowDef, map[string]any{}, runner.WithObserver(observer))

	require.NoError(t, err)
	encoded, err := json.Marshal(observer.results["each"])
	require.NoError(t, err)
	var decoded struct {
		HTTPCalls []spi.HTTPCallGroup `json:"http_calls"`
	}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Len(t, decoded.HTTPCalls, 1)
	assert.Equal(t, 2, decoded.HTTPCalls[0].Count())
}

func TestRun_RedirectsCountAgainstTheLimit(t *testing.T) {
	for _, kind := range []string{"request", "sse"} {
		t.Run(kind, func(t *testing.T) {
			var received atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/end?token=private", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: done\n\n"))
			}))
			defer server.Close()
			definition := fmt.Sprintf(`{"version":"1.0","name":"redirect","nodes":[
				{"id":"get","type":%q,"data":{"method":"GET","url":%q}}
			],"edges":[]}`, kind, server.URL+"/start")

			result, err := runner.Run(parseFlow(t, definition), nil, runner.WithHTTPCallLimit(1))
			require.Error(t, err)
			assert.Equal(t, int32(1), received.Load(), "the redirected call must be refused before sending")
			encoded, encodeErr := json.Marshal(result.ExecutionResults["get"])
			require.NoError(t, encodeErr)
			var wire struct {
				ErrorCode string `json:"error_code"`
			}
			require.NoError(t, json.Unmarshal(encoded, &wire))
			assert.Equal(t, httpcall.LimitExceededCode, wire.ErrorCode)
			groups := httpCalls(t, result, "get")
			require.Len(t, groups, 1)
			assert.Equal(t, []int{http.StatusFound}, groups[0].StatusCodes)

			received.Store(0)
			result, err = runner.Run(parseFlow(t, definition), nil, runner.WithHTTPCallLimit(2))
			require.NoError(t, err)
			assert.Equal(t, int32(2), received.Load())
			groups = httpCalls(t, result, "get")
			require.Len(t, groups, 1)
			assert.Equal(t, "/start", groups[0].PathTemplate)
			assert.Equal(t, []int{http.StatusFound, http.StatusOK}, groups[0].StatusCodes)
		})
	}
}

func TestRun_HTTPCallKeepsPartialResponseBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "20")
		_, _ = w.Write([]byte("partial"))
	}))
	defer server.Close()
	definition := parseFlow(t, `{"version":"1.0","name":"partial","nodes":[`+
		requestNode("get", server.URL)+`],"edges":[]}`)
	result, err := runner.Run(definition, nil)
	require.Error(t, err)
	groups := httpCalls(t, result, "get")
	require.Len(t, groups, 1)
	assert.Equal(t, []int{http.StatusOK}, groups[0].StatusCodes)
	assert.Equal(t, []int64{7}, groups[0].ResponseBytes)
}

func TestRun_ModuleCallsShareTheExecutionLimit(t *testing.T) {
	server := statusServer(t)
	definition := parseFlow(t, `{"version":"1.0","name":"parent","nodes":[
		{"id":"child","type":"module","data":{"flow_id":"child-flow"}}
	],"edges":[]}`)
	refs := flow.ReferencedFlowRegistry{
		"child-flow": {FlowDefinition: []byte(loopFlow(server.URL+"/items/{{item}}", 3))},
	}
	result, err := runner.Run(definition, nil, runner.WithReferencedFlows(refs), runner.WithHTTPCallLimit(2))
	require.Error(t, err)
	groups := httpCalls(t, result, "child")
	require.Len(t, groups, 1)
	assert.Equal(t, 2, groups[0].Count())
	assert.Equal(t, "get", groups[0].NodeID)
	encoded, encodeErr := json.Marshal(result.ExecutionResults["child"])
	require.NoError(t, encodeErr)
	assert.Contains(t, string(encoded), `"error_code":"HTTP_CALL_LIMIT_EXCEEDED"`)
}

func TestRun_ParallelCallsCannotPassTheLimit(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	nodes := make([]string, 10)
	for i := range nodes {
		nodes[i] = requestNode(strconv.Itoa(i), server.URL)
	}
	definition := parseFlow(t, `{"version":"1.0","name":"parallel","nodes":[`+
		strings.Join(nodes, ",")+`],"edges":[]}`)
	result, err := runner.Run(definition, nil, runner.WithHTTPCallLimit(3))
	require.Error(t, err)
	assert.Equal(t, int32(3), received.Load())
	count := 0
	for id := range result.ExecutionResults {
		for _, group := range httpCalls(t, result, id) {
			count += group.Count()
		}
	}
	assert.Equal(t, 3, count)
}

func TestRun_RetryCallsSurviveTheLastAttempt(t *testing.T) {
	server := statusServer(t)
	definition := parseFlow(t, `{"version":"1.0","name":"retry","nodes":[
  {"id":"get","type":"request","data":{"method":"GET","url":"`+server.URL+`"},
   "assertions":[{"extractor_type":"status_code","operator_type":"equals","operator_data":{"value":201}}]}
 ],"edges":[]}`)
	result, err := runner.Run(definition, nil, runner.WithMiddleware(runner.Retry(3)))
	require.Error(t, err)
	groups := httpCalls(t, result, "get")
	require.Len(t, groups, 1)
	assert.Equal(t, []int{200, 200, 200}, groups[0].StatusCodes)
}
