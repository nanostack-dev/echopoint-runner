package flow

import (
	"encoding/json"
	"fmt"
)

// ReferencedFlow contains the extra flow definitions the runner can execute from
// module nodes without calling back into the control plane at runtime.
type ReferencedFlow struct {
	FlowDefinition json.RawMessage        `json:"flow_definition"`
	InputOverrides map[string]interface{} `json:"input_overrides,omitempty"`
}

// ReferencedFlowRegistry stores module targets by flow ID.
type ReferencedFlowRegistry map[string]ReferencedFlow

func (r *ReferencedFlow) UnmarshalJSON(data []byte) error {
	type referencedFlowAlias struct {
		FlowDefinition json.RawMessage        `json:"flow_definition"`
		InputOverrides map[string]interface{} `json:"input_overrides,omitempty"`
		Environment    map[string]string      `json:"environment,omitempty"`
	}

	var raw referencedFlowAlias
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal referenced flow: %w", err)
	}

	inputOverrides := raw.InputOverrides
	if len(inputOverrides) == 0 && len(raw.Environment) > 0 {
		inputOverrides = make(map[string]interface{}, len(raw.Environment))
		for key, value := range raw.Environment {
			inputOverrides[key] = value
		}
	}

	r.FlowDefinition = raw.FlowDefinition
	r.InputOverrides = inputOverrides
	return nil
}
