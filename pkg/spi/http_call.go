package spi

import "time"

// HTTPCallErrorClass says why an HTTP call got no response. A call that got a
// response has none: its status code says whether it failed.
type HTTPCallErrorClass string

const (
	HTTPCallErrorDNS     HTTPCallErrorClass = "dns"
	HTTPCallErrorConnect HTTPCallErrorClass = "connect"
	HTTPCallErrorTLS     HTTPCallErrorClass = "tls"
	HTTPCallErrorTimeout HTTPCallErrorClass = "timeout"
)

// HTTPCallGroup is every HTTP call one node sent to one method, host and path
// template during a node result, in start order. Loop iterations, poll
// attempts and retries of the same request share a group, so the fixed fields
// are stored once and the varying ones as parallel arrays: index i of every
// array describes call i.
//
// A group never holds the resolved URL, query string, headers or body.
// PathTemplate is the path of the node's URL template before resolution.
type HTTPCallGroup struct {
	// NodeID is the node that sent the calls. Inside a loop, poll or module body
	// it is a node of that body, not the node whose result holds the group.
	NodeID       string `json:"node_id"`
	Method       string `json:"method"`
	Host         string `json:"host"`
	PathTemplate string `json:"path_template"`
	// StartedAt is when the first call started; OffsetsMs[i] is when call i
	// started, in milliseconds after it.
	StartedAt   time.Time `json:"started_at"`
	OffsetsMs   []int64   `json:"offsets_ms"`
	DurationsMs []int64   `json:"durations_ms"`
	// StatusCodes[i] is 0 when call i got no response; TransportErrors then
	// says why, keyed by the decimal index.
	StatusCodes     []int                         `json:"status_codes"`
	TransportErrors map[string]HTTPCallErrorClass `json:"transport_errors,omitempty"`
	// ResponseBytes is absent for streamed responses (SSE).
	ResponseBytes []int64 `json:"response_bytes,omitempty"`
}

// Count is the number of calls in the group.
func (g HTTPCallGroup) Count() int {
	return len(g.OffsetsMs)
}
