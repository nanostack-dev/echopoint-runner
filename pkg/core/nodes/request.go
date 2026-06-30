package nodes

import (
	"context"
	"fmt"
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

func runRequest(ctx context.Context, cfg RequestCfg, rt node.Runtime) (node.Result, error) {
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
		return node.Result{}, fmt.Errorf("build request: %w", node.ErrUser)
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := rt.HTTP.Do(req)
	if err != nil {
		return node.Result{}, fmt.Errorf("http: %w", node.ErrUser)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	return node.Result{
		Outputs: value.Map{"status": value.Of(resp.StatusCode)},
		Assert:  value.JSON(raw),
	}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindRequest, runRequest) }
