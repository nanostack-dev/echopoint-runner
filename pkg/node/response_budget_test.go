package node_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const kibibyte = 1 << 10

// bodyNode reads its whole response body through a json_path assertion.
func bodyNode(t *testing.T, url string, timeoutMs int) *node.RequestNode {
	t.Helper()
	parsed, err := node.UnmarshalNode([]byte(`{"id":"export","type":"request","data":{` +
		`"method":"GET","url":"` + url + `","timeout":` + strconv.Itoa(timeoutMs) + `},` +
		`"assertions":[{"extractor_type":"json_path","extractor_data":{"path":"$.ok"},` +
		`"operator_type":"equals","operator_data":{"value":true}}]}`))
	require.NoError(t, err)
	reqNode, ok := node.AsRequestNode(parsed)
	require.True(t, ok)
	return reqNode
}

func jsonOfSize(size int) []byte {
	prefix := `{"ok":true,"padding":"`
	return []byte(prefix + strings.Repeat("x", size-len(prefix)-2) + `"}`)
}

// serveWithLength answers body with its Content-Length, as most APIs do.
func serveWithLength(t *testing.T, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func budgetContext(budget *node.ResponseBudget) spi.ExecutionContext {
	return spi.ExecutionContext{Ctx: node.WithResponseBudget(context.Background(), budget)}
}

func TestResponseBudgetHoldsABodyUntilItsResponseIsReleased(t *testing.T) {
	body := jsonOfSize(100 * kibibyte)
	budget := node.NewResponseBudget(mebibyte)
	reqNode := bodyNode(t, serveWithLength(t, body), 5000)

	res, err := reqNode.Execute(budgetContext(budget))
	require.NoError(t, err)

	assert.Equal(
		t,
		int64(2*len(body)),
		budget.InUse(),
		"a kept body counts once for its bytes, once for its parsed form",
	)
	require.NoError(t, applyEnginePass(reqNode, res))
	res.(node.ResponseReleaser).ReleaseResponse()
	assert.Zero(t, budget.InUse())
	assert.Nil(t, res.(*node.RequestExecutionResult).AssertionContext())
}

func TestResponseBudgetKeepsOnlyTheReportedBodyWhenNothingReadsIt(t *testing.T) {
	body := jsonOfSize(3 * mebibyte)
	budget := node.NewResponseBudget(64 * mebibyte)
	parsed, err := node.UnmarshalNode([]byte(`{"id":"export","type":"request","data":{` +
		`"method":"GET","url":"` + serveWithLength(t, body) + `","timeout":5000},` +
		`"assertions":[{"extractor_type":"status_code","operator_type":"equals","operator_data":{"value":200}}]}`))
	require.NoError(t, err)

	res, err := parsed.Execute(budgetContext(budget))
	require.NoError(t, err)

	assert.Equal(t, int64(2*node.ReportedResponseBodyBytes), budget.InUse())
	result := res.(*node.RequestExecutionResult)
	assert.Equal(t, body[:node.ReportedResponseBodyBytes], result.ResponseBody)
	assert.True(t, result.ResponseBodyTruncated)
	assert.Nil(t, result.ResponseBodyParsed)
	result.ReleaseResponse()
	assert.Zero(t, budget.InUse())
}

func TestResponseBudgetMakesABodyWaitUntilBytesAreReleased(t *testing.T) {
	body := jsonOfSize(100 * kibibyte)
	budget := node.NewResponseBudget(mebibyte)
	release := node.HoldResponseBudgetForTest(budget, mebibyte)
	reqNode := bodyNode(t, serveWithLength(t, body), 5000)

	done := make(chan error, 1)
	go func() {
		_, err := reqNode.Execute(budgetContext(budget))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the body was read without budget: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the body never got budget once it was released")
	}
}

func TestResponseBudgetWaitEndsWithTheRequestTimeout(t *testing.T) {
	budget := node.NewResponseBudget(mebibyte)
	release := node.HoldResponseBudgetForTest(budget, mebibyte)
	defer release()
	reqNode := bodyNode(t, serveWithLength(t, jsonOfSize(100*kibibyte)), 200)

	started := time.Now()
	res, err := reqNode.Execute(budgetContext(budget))

	require.Error(t, err)
	assert.Less(t, time.Since(started), 2*time.Second)
	assert.Equal(t, "REQUEST_TIMEOUT", spi.ErrorCode(err))
	assert.Contains(t, *res.(*node.RequestExecutionResult).ErrorMsg, "timed out waiting for runner memory")
	assert.Equal(t, int64(mebibyte), budget.InUse(), "a body that never got budget holds none")
}

func TestResponseBudgetIsReturnedWhenTheBodyIsTooLarge(t *testing.T) {
	budget := node.NewResponseBudget(64 * mebibyte)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No Content-Length: the size is only known once the read passes the limit.
		for range 3 {
			_, _ = w.Write(jsonOfSize(600 * kibibyte))
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(server.Close)
	reqNode := bodyNode(t, server.URL, 5000)
	ctx := spi.ExecutionContext{Ctx: node.WithMaxResponseBytes(
		node.WithResponseBudget(context.Background(), budget), mebibyte,
	)}

	_, err := reqNode.Execute(ctx)

	require.Error(t, err)
	assert.Equal(t, node.ResponseTooLargeCode, spi.ErrorCode(err))
	assert.Zero(t, budget.InUse())
}

func TestResponseBudgetReadsABodyWithoutContentLengthWhole(t *testing.T) {
	body := jsonOfSize(300 * kibibyte)
	budget := node.NewResponseBudget(64 * mebibyte)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for start := 0; start < len(body); start += 100 * kibibyte {
			_, _ = w.Write(body[start:min(start+100*kibibyte, len(body))])
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(server.Close)
	reqNode := bodyNode(t, server.URL, 5000)

	res, err := reqNode.Execute(budgetContext(budget))

	require.NoError(t, err)
	require.NoError(t, applyEnginePass(reqNode, res))
	result := res.(*node.RequestExecutionResult)
	assert.Equal(t, body, result.ResponseBody)
	assert.NotNil(t, result.ResponseBodyParsed)
	assert.Equal(t, int64(2*cap(result.ResponseBody)), budget.InUse())
	result.ReleaseResponse()
	assert.Zero(t, budget.InUse())
}
