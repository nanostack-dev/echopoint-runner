package flow

import "encoding/json"

// ReferencedFlow contains the extra flow definitions the runner can execute from
// module nodes without calling back into the control plane at runtime.
type ReferencedFlow struct {
	FlowDefinition json.RawMessage   `json:"flow_definition"`
	Environment    map[string]string `json:"environment,omitempty"`
}

// ReferencedFlowRegistry stores module targets by flow ID.
type ReferencedFlowRegistry map[string]ReferencedFlow
