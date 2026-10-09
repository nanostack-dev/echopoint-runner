package spi

import "time"

// HTTPCallErrorClass says why an HTTP call failed. An empty class is a call
// that got a 1xx-3xx response.
type HTTPCallErrorClass string

const (
	HTTPCallErrorDNS     HTTPCallErrorClass = "dns"
	HTTPCallErrorConnect HTTPCallErrorClass = "connect"
	HTTPCallErrorTLS     HTTPCallErrorClass = "tls"
	HTTPCallErrorTimeout HTTPCallErrorClass = "timeout"
	HTTPCallError4xx     HTTPCallErrorClass = "http_4xx"
	HTTPCallError5xx     HTTPCallErrorClass = "http_5xx"
)

// HTTPCall is one outbound HTTP request a node sent during an execution. Each
// loop iteration, poll attempt and retry is its own call.
//
// It never holds the resolved URL, query string, headers or body. PathTemplate
// is the path of the node's URL template before resolution, so a secret
// resolved into the URL cannot leak through it.
type HTTPCall struct {
	// Seq orders the calls of one execution, starting at 1.
	Seq    int    `json:"seq"`
	NodeID string `json:"node_id"`
	Method string `json:"method"`
	// Host is the resolved host, with its port when the URL names one.
	Host         string             `json:"host"`
	PathTemplate string             `json:"path_template"`
	StatusCode   *int               `json:"status_code,omitempty"`
	ErrorClass   HTTPCallErrorClass `json:"error_class,omitempty"`
	StartedAt    time.Time          `json:"started_at"`
	DurationMs   int64              `json:"duration_ms"`
	// ResponseBytes is the body size read, or nil for a streamed response.
	ResponseBytes *int64 `json:"response_bytes,omitempty"`
}
