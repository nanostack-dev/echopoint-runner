package node_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mebibyte = 1 << 20

func serveJSON(t *testing.T, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func jsonArrayOfSize(minBytes int) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i := 0; buf.Len() < minBytes; i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		fmt.Fprintf(&buf, `{"id":"item_%07d","padding":"%s"}`, i, strings.Repeat("x", 64))
	}
	buf.WriteString(`,{"id":"last"}]`)
	return buf.Bytes()
}

func TestRequestNode_FailsWithResponseTooLargeAboveTheDefaultLimit(t *testing.T) {
	url := serveJSON(t, bytes.Repeat([]byte("a"), 10*mebibyte+1))
	parsed, err := node.UnmarshalNode([]byte(`{"id":"export","type":"request","data":{` +
		`"method":"GET","url":"` + url + `/export","timeout":10000}}`))
	require.NoError(t, err)

	res, err := parsed.Execute(spi.ExecutionContext{Ctx: context.Background()})

	require.Error(t, err)
	assert.Equal(t, "RESPONSE_TOO_LARGE", spi.ErrorCode(err))
	require.NotNil(t, res.(*node.RequestExecutionResult).ErrorCode)
	assert.Equal(t, "RESPONSE_TOO_LARGE", *res.(*node.RequestExecutionResult).ErrorCode)
	assert.Contains(t, *res.(*node.RequestExecutionResult).ErrorMsg, "10 MiB")
}

func TestRequestNode_ReportsABoundedBodyForALargeAllowedResponse(t *testing.T) {
	body := jsonArrayOfSize(3 * mebibyte)
	url := serveJSON(t, body)
	parsed, err := node.UnmarshalNode([]byte(`{"id":"export","type":"request","data":{` +
		`"method":"GET","url":"` + url + `/export","timeout":10000},` +
		`"assertions":[{"extractor_type":"json_path","extractor_data":{"path":"$[-1:].id"},` +
		`"operator_type":"equals","operator_data":{"value":"last"}}]}`))
	require.NoError(t, err)
	reqNode, ok := node.AsRequestNode(parsed)
	require.True(t, ok)

	res, err := reqNode.Execute(spi.ExecutionContext{Ctx: context.Background()})
	require.NoError(t, err)
	require.NoError(t, applyEnginePass(reqNode, res))

	encoded, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Less(t, len(encoded), 2*mebibyte)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(encoded, &wire))
	assert.Equal(t, true, wire["response_body_truncated"])
	assert.NotContains(t, wire, "response_body_parsed")
}
