package nodes

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// RequestCfg configures an HTTP request node. Its declared assertions and
// outputs (on the embedded Base) run against the response body — the node never
// mentions them.
type RequestCfg struct {
	node.Base

	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

func runRequest(ctx context.Context, cfg RequestCfg, _ value.Value, rt node.Runtime) (node.Result, error) {
	method := cfg.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if cfg.Body != "" {
		body = strings.NewReader(cfg.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, body)
	if err != nil {
		return node.Result{}, node.UserErrf("REQUEST_FAILED", "build request: %v", err)
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := rt.HTTP.Do(req)
	if err != nil {
		return node.Result{}, node.UserErrf("REQUEST_FAILED", "http: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	// Expose the whole response as {status, headers, body} so this node's own
	// assertions AND any downstream node can path into status/headers/body.
	respMap := value.Map{
		"status":  value.Of(resp.StatusCode),
		"headers": value.Of(headerMap(resp.Header)),
		"body":    value.Of(parseBody(raw)),
	}
	return node.Result{Outputs: respMap, Assert: respMap.Value()}, nil
}

// parseBody parses the body as JSON when it is valid JSON, otherwise keeps it as
// a raw string — so assertions can path into "body.x" (JSON) or compare "body"
// directly (text). Content-Type is not trusted (many APIs mislabel it).
func parseBody(raw []byte) any {
	if v := value.JSON(raw); !v.IsZero() {
		return v.Raw()
	}
	return string(raw)
}

// headerMap flattens response headers (first value, lowercased key) so
// assertions can path into "headers.content-type".
func headerMap(h http.Header) map[string]any {
	out := make(map[string]any, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[strings.ToLower(k)] = v[0]
		}
	}
	return out
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindRequest, runRequest) }
