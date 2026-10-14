package runner_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const mebibyte = 1 << 20

const (
	statusAssertion = `{"extractor_type":"status_code","operator_type":"equals","operator_data":{"value":200}}`
	// bodyAssertion reads the whole body, so the node keeps and parses it.
	bodyAssertion = `{"extractor_type":"json_path","extractor_data":{"path":"$.ok"},` +
		`"operator_type":"equals","operator_data":{"value":true}}`
)

// largeJSON is a JSON object of size bytes that starts with prefix.
func largeJSON(size int, prefix string) []byte {
	head := `{"ok":true,"prefix":"` + prefix + `","padding":"`
	return []byte(head + strings.Repeat("x", size-len(head)-2) + `"}`)
}

func serveLarge(t *testing.T, body []byte) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func parallelRequests(t *testing.T, url string, nodes int, assertion string) string {
	t.Helper()
	defs := make([]string, nodes)
	for i := range defs {
		defs[i] = fmt.Sprintf(`{"id":"get_%d","type":"request","data":{"method":"GET","url":%q},"assertions":[%s]}`,
			i, url, assertion)
	}
	return `{"version":"1.0","name":"large","nodes":[` + strings.Join(defs, ",") + `],"edges":[]}`
}

type reportedResponse struct {
	ResponseBody          []byte              `json:"response_body"`
	ResponseBodyParsed    any                 `json:"response_body_parsed"`
	ResponseBodyTruncated bool                `json:"response_body_truncated"`
	HTTPCalls             []spi.HTTPCallGroup `json:"http_calls"`
	ErrorCode             string              `json:"error_code"`
}

func reported(t *testing.T, result *spi.FlowExecutionResult, nodeID string) reportedResponse {
	t.Helper()
	encoded, err := json.Marshal(result.ExecutionResults[nodeID])
	require.NoError(t, err)
	var decoded reportedResponse
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return decoded
}

func liveHeap() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

func TestRunKeepsOnlyTheReportedBodyOfLargeResponses(t *testing.T) {
	const nodes = 4
	url := serveLarge(t, largeJSON(8*mebibyte, ""))
	definition := parseFlow(t, parallelRequests(t, url, nodes, bodyAssertion))
	before := liveHeap()

	result, err := runner.Run(definition, nil)
	require.NoError(t, err)

	retained := int64(liveHeap()) - int64(before)
	runtime.KeepAlive(result)
	t.Logf("%d finished nodes of 8 MiB responses retain %.1f MiB", nodes, float64(retained)/mebibyte)
	assert.Less(t, retained, int64(nodes*2*mebibyte),
		"a finished node keeps its 1 MiB reported body, not its 8 MiB response and parsed tree")
}

func TestRunReportsALargeResponseTheSameWhetherItsBodyIsReadOrNot(t *testing.T) {
	body := largeJSON(3*mebibyte, "")
	url := serveLarge(t, body)
	var reports []reportedResponse
	for _, assertion := range []string{statusAssertion, bodyAssertion} {
		result, err := runner.Run(parseFlow(t, parallelRequests(t, url, 1, assertion)), nil)
		require.NoError(t, err)
		reports = append(reports, reported(t, result, "get_0"))
	}

	for _, report := range reports {
		assert.Equal(t, body[:node.ReportedResponseBodyBytes], report.ResponseBody)
		assert.True(t, report.ResponseBodyTruncated)
		assert.Nil(t, report.ResponseBodyParsed)
		require.Len(t, report.HTTPCalls, 1)
		assert.Equal(t, []int64{int64(len(body))}, report.HTTPCalls[0].ResponseBytes)
	}
}

func TestRunMasksASecretEchoedInALargeResponse(t *testing.T) {
	url := serveLarge(t, largeJSON(3*mebibyte, secretValue))
	for name, assertion := range map[string]string{"status": statusAssertion, "body": bodyAssertion} {
		t.Run(name, func(t *testing.T) {
			definition := parseFlow(t, parallelRequests(t, url, 1, assertion), "apiToken")

			result, err := runner.Run(definition, map[string]any{"apiToken": secretValue},
				runner.WithSecretInputKeys([]string{"apiToken"}))

			require.NoError(t, err)
			body := reported(t, result, "get_0").ResponseBody
			assert.NotContains(t, string(body), secretValue)
			assert.Contains(t, string(body), `"prefix":"***"`)
		})
	}
}

func TestRunReturnsTheResponseBudgetAfterEveryOutcome(t *testing.T) {
	large := serveLarge(t, largeJSON(2*mebibyte, ""))
	small := serveLarge(t, []byte(`{"ok":false}`))
	refused := httptest.NewServer(http.NotFoundHandler())
	refused.Close()
	cases := map[string]struct {
		definition string
		options    []runner.Option
	}{
		"passing body assertion":   {definition: parallelRequests(t, large, 3, bodyAssertion)},
		"passing status assertion": {definition: parallelRequests(t, large, 3, statusAssertion)},
		"failing assertion":        {definition: parallelRequests(t, small, 1, bodyAssertion)},
		"retried failing assertion": {
			definition: parallelRequests(t, small, 1, bodyAssertion),
			options:    []runner.Option{runner.WithMiddleware(runner.Retry(3))},
		},
		"output extraction failure": {definition: `{"version":"1.0","name":"t","nodes":[` +
			`{"id":"get","type":"request","data":{"method":"GET","url":"` + large + `"},` +
			`"outputs":[{"name":"missing","extractor":{"type":"json_path","path":"$.missing"}}]}],"edges":[]}`},
		"response too large": {
			definition: parallelRequests(t, large, 1, bodyAssertion),
			options:    []runner.Option{runner.WithMaxResponseBytes(mebibyte)},
		},
		"connection refused": {definition: parallelRequests(t, refused.URL, 1, bodyAssertion)},
		"loop body": {definition: `{"version":"1.0","name":"t","nodes":[{"id":"each","type":"loop","data":{` +
			`"items":[1,2,3],"body":{"nodes":[{"id":"get","type":"request","data":{"method":"GET","url":"` + large +
			`"},"assertions":[` + bodyAssertion + `]}],"edges":[]}}}],"edges":[]}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			budget := node.NewResponseBudget(16 * mebibyte)

			_, _ = runner.Run(
				parseFlow(t, tc.definition),
				nil,
				append(tc.options, runner.WithResponseBudget(budget))...)

			assert.Zero(t, budget.InUse())
		})
	}
}

func TestRunSharesTheResponseBudgetAcrossParallelNodes(t *testing.T) {
	url := serveLarge(t, largeJSON(2*mebibyte, ""))
	// Room for one kept body and its parsed form at a time.
	budget := node.NewResponseBudget(5 * mebibyte)

	result, err := runner.Run(parseFlow(t, parallelRequests(t, url, 6, bodyAssertion)), nil,
		runner.WithResponseBudget(budget))

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Zero(t, budget.InUse())
}
