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

// nodeFinishedPayload carries the node's full execution result. Result is the
// engine's own AnyExecutionResult (the single source of truth) — kept typed so
// the wire shape stays compile-checked, while serializing to the flat object the
// control plane decodes with DecodeAnyExecutionResult. Every field
// (request/response, assertion_results, skip_reason, missing_inputs,
// error_message, outputs) is preserved without a parallel, lossy struct.
type nodeFinishedPayload struct {
	NodeID      string                  `json:"nodeId"`
	DisplayName string                  `json:"displayName"`
	Duration    int64                   `json:"duration"`
	Timestamp   string                  `json:"timestamp"`
	Success     *bool                   `json:"success,omitempty"`
	Result      node.AnyExecutionResult `json:"result,omitempty"`
	Error       string                  `json:"error,omitempty"`
}
