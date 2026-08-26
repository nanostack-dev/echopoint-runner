package redact

import (
	"encoding/json"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// maskedResult reports a node result whose every field is free of secret
// values. It serializes as the original result's JSON with the secrets replaced,
// which is why it needs no knowledge of the concrete result types and their
// request/response fields.
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
	encoded, err := json.Marshal(result)
	if err != nil {
		return result
	}
	return &maskedResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      result.GetNodeID(),
			DisplayName: result.GetDisplayName(),
			NodeType:    result.GetNodeType(),
			Inputs:      r.Map(result.GetInputs()),
			Outputs:     r.Map(result.GetOutputs()),
			Error:       r.Error(result.GetError()),
			ExecutedAt:  result.GetExecutedAt(),
		},
		encoded: json.RawMessage(r.Text(string(encoded))),
	}
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
