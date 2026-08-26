package redact

import (
	"encoding/json"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// maskedResult reports a node result whose every field is free of secret
// values. It serializes as the original result's JSON tree with the secrets
// replaced, which is why it needs no knowledge of the concrete result types and
// their request/response fields.
type maskedResult struct {
	spi.BaseExecutionResult

	encoded json.RawMessage
}

func (m *maskedResult) MarshalJSON() ([]byte, error) {
	return m.encoded, nil
}

// Result returns a masked copy of result. The original is left untouched: while
// a flow runs it is still the engine's live value, feeding downstream nodes.
func (r *Redactor) Result(result spi.AnyResult) spi.AnyResult {
	if r == nil || result == nil {
		return result
	}
	base := spi.BaseExecutionResult{
		NodeID:      result.GetNodeID(),
		DisplayName: result.GetDisplayName(),
		NodeType:    result.GetNodeType(),
		Inputs:      r.Map(result.GetInputs()),
		Outputs:     r.Map(result.GetOutputs()),
		Error:       r.Error(result.GetError()),
		ExecutedAt:  result.GetExecutedAt(),
	}
	return &maskedResult{BaseExecutionResult: base, encoded: r.encodeMasked(result, base)}
}

// encodeMasked serializes the masked form of result. Any failure falls back to
// the masked base fields alone: losing the node-kind detail is the closed side
// of this seam, reporting the unmasked original is not.
func (r *Redactor) encodeMasked(result spi.AnyResult, base spi.BaseExecutionResult) json.RawMessage {
	if tree, err := jsonTree(result); err == nil {
		if encoded, marshalErr := json.Marshal(r.mask(tree)); marshalErr == nil {
			return encoded
		}
	}
	fallback, err := json.Marshal(base)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return fallback
}

// FlowResult returns a masked copy of the flow result and of every node result
// it carries.
func (r *Redactor) FlowResult(result *spi.FlowExecutionResult) *spi.FlowExecutionResult {
	if r == nil || result == nil {
		return result
	}
	masked := *result
	masked.ExecutionResults = make(map[string]spi.AnyResult, len(result.ExecutionResults))
	for nodeID, nodeResult := range result.ExecutionResults {
		masked.ExecutionResults[nodeID] = r.Result(nodeResult)
	}
	masked.FinalOutputs = r.Map(result.FinalOutputs)
	masked.Error = r.Error(result.Error)
	if result.ErrorMsg != nil {
		maskedMsg := r.Text(*result.ErrorMsg)
		masked.ErrorMsg = &maskedMsg
	}
	return &masked
}
