package node_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reportedWire is the request result as a control plane receives it.
func reportedWire(t *testing.T, contentType string, body []byte) map[string]any {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	reqNode, err := node.UnmarshalNode([]byte(`{"id":"get","type":"request","data":{` +
		`"method":"GET","url":"` + srv.URL + `","timeout":5000}}`))
	require.NoError(t, err)

	res, err := reqNode.Execute(spi.ExecutionContext{Ctx: context.Background()})
	require.NoError(t, err)
	encoded, err := json.Marshal(res)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(encoded, &wire))
	return wire
}

// A body whose parsed form carries it whole is reported once, as that parsed
// form: the base64 copy only doubled the result.
func TestRequestResult_ReportsAParsedBodyWithoutItsRawCopy(t *testing.T) {
	for name, tc := range map[string]struct {
		contentType string
		body        string
		parsed      any
	}{
		"json object":       {"application/json", `{"id":"cus_1","n":2}`, map[string]any{"id": "cus_1", "n": 2.0}},
		"json string":       {"application/json; charset=utf-8", `"ok"`, "ok"},
		"text":              {"text/plain", "upstream unavailable — retry after 30s", "upstream unavailable — retry after 30s"},
		"html":              {"text/html", "<p>hi</p>", "<p>hi</p>"},
		"json that fails":   {"application/json", `{"broken"`, `{"broken"`},
		"empty json":        {"application/json", ``, ""},
		"json with bom":     {"application/json", "\xEF\xBB\xBF{\"a\":1}", map[string]any{"a": 1.0}},
		"nested node blobs": {"application/json", `{"result":{"response_body_parsed":{"x":1}}}`, map[string]any{"result": map[string]any{"response_body_parsed": map[string]any{"x": 1.0}}}},
	} {
		t.Run(name, func(t *testing.T) {
			wire := reportedWire(t, tc.contentType, []byte(tc.body))

			assert.NotContains(t, wire, "response_body")
			assert.Equal(t, tc.parsed, wire["response_body_parsed"])
			assert.NotContains(t, wire, "response_body_truncated")
		})
	}
}

// A body the parsed form would alter keeps its exact bytes in response_body:
// JSON text cannot hold invalid UTF-8, and Echopoint stores a NUL as U+FFFD.
// A JSON null parses to nothing, so the raw body is the only report of it.
func TestRequestResult_KeepsTheRawBodyWhenTheParsedFormIsLossy(t *testing.T) {
	for name, tc := range map[string]struct {
		contentType string
		body        []byte
	}{
		"png":                  {"image/png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")},
		"latin-1 text":         {"text/plain; charset=iso-8859-1", []byte("caf\xe9")},
		"json with bad utf-8":  {"application/json", []byte("{\"name\":\"caf\xe9\"}")},
		"text with a nul byte": {"text/plain", []byte("a\x00b")},
		"json null":            {"application/json", []byte("null")},
	} {
		t.Run(name, func(t *testing.T) {
			wire := reportedWire(t, tc.contentType, tc.body)

			encoded, ok := wire["response_body"].(string)
			require.True(t, ok, "response_body should be reported, got %v", wire["response_body"])
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			require.NoError(t, err)
			assert.Equal(t, tc.body, decoded)
		})
	}
}

// A truncated body has no parsed form, so its first bytes stay in response_body.
func TestRequestResult_KeepsTheRawPrefixOfATruncatedBody(t *testing.T) {
	body := jsonArrayOfSize(2 * mebibyte)

	wire := reportedWire(t, "application/json", body)

	encoded, ok := wire["response_body"].(string)
	require.True(t, ok)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(body[:node.ReportedResponseBodyBytes], decoded))
	assert.NotContains(t, wire, "response_body_parsed")
	assert.Equal(t, true, wire["response_body_truncated"])
}
