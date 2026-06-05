package runtime

import "github.com/nanostack-dev/echopoint-runner/pkg/node"

// The structs below are the typed shapes of the progress-event payloads emitted
// to the control plane. Their JSON tags are the wire contract — they must match
// the keys the control plane consumes exactly. They replace hand-built
// map[string]any literals so field names are checked at compile time.

type flowStartedPayload struct {
	ExecutionID string `json:"execution_id"`
	FlowName    string `json:"flowName"`
	Timestamp   string `json:"timestamp"`
}

type nodeStartedPayload struct {
	NodeID      string `json:"nodeId"`
	DisplayName string `json:"displayName"`
	NodeType    string `json:"nodeType"`
	Timestamp   string `json:"timestamp"`
}

type nodeFinishedPayload struct {
	NodeID      string             `json:"nodeId"`
	DisplayName string             `json:"displayName"`
	Duration    int64              `json:"duration"`
	Timestamp   string             `json:"timestamp"`
	Success     *bool              `json:"success,omitempty"`
	Result      *nodeResultPayload `json:"result,omitempty"`
	Error       string             `json:"error,omitempty"`
}

// nodeResultPayload mirrors a node's execution result. duration_ms is set for
// request nodes, delay_ms for delay nodes, request/response only for requests —
// hence the pointers with omitempty.
type nodeResultPayload struct {
	NodeID      string         `json:"node_id"`
	DisplayName string         `json:"display_name"`
	NodeType    string         `json:"node_type"`
	Outputs     map[string]any `json:"outputs"`
	Request     *requestInfo   `json:"request,omitempty"`
	Response    *responseInfo  `json:"response,omitempty"`
	DurationMs  *int64         `json:"duration_ms,omitempty"`
	DelayMs     *int64         `json:"delay_ms,omitempty"`
}

type requestInfo struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type responseInfo struct {
	StatusCode       int                    `json:"status_code"`
	Headers          map[string][]string    `json:"headers"`
	AssertionResults []node.AssertionResult `json:"assertion_results,omitempty"`
}
