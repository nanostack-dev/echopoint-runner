package redact

import (
	"encoding/json"
	"maps"

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
	return r.load().result(result)
}

func (set *secretSet) result(result spi.AnyResult) spi.AnyResult {
	if set == nil || result == nil {
		return result
	}
	base := spi.BaseExecutionResult{
		NodeID:      result.GetNodeID(),
		DisplayName: result.GetDisplayName(),
		NodeType:    result.GetNodeType(),
		Inputs:      set.mapValues(result.GetInputs()),
		Outputs:     set.mapValues(result.GetOutputs()),
		Error:       set.maskError(result.GetError()),
		ExecutedAt:  result.GetExecutedAt(),
	}
	return &maskedResult{BaseExecutionResult: base, encoded: set.encodeMasked(result, base)}
}

// encodeMasked serializes the masked form of result. Any failure falls back to
// the masked base fields alone: losing the node-kind detail is the closed side
// of this seam, reporting the unmasked original is not.
func (set *secretSet) encodeMasked(result spi.AnyResult, base spi.BaseExecutionResult) json.RawMessage {
	if tree, err := jsonTree(result); err == nil {
		identity := identityFields(tree)
		masked := set.mask(tree)
		if fields, ok := masked.(map[string]any); ok {
			maps.Copy(fields, identity)
		}
		if encoded, marshalErr := json.Marshal(masked); marshalErr == nil {
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
	set := r.load()
	if set == nil || result == nil {
		return result
	}
	masked := *result
	masked.ExecutionResults = make(map[string]spi.AnyResult, len(result.ExecutionResults))
	for nodeID, nodeResult := range result.ExecutionResults {
		masked.ExecutionResults[nodeID] = set.result(nodeResult)
	}
	masked.FinalOutputs = set.mapValues(result.FinalOutputs)
	masked.Error = set.maskError(result.Error)
	if result.ErrorMsg != nil {
		maskedMsg := set.text(*result.ErrorMsg)
		masked.ErrorMsg = &maskedMsg
	}
	return &masked
}

// identityFields copies the fields the runner writes itself, never a target or
// a flow. Masking them could only corrupt the report: a short secret such as
// "1" would rewrite executed_at, and the control plane could no longer decode it.
func identityFields(tree any) map[string]any {
	fields, ok := tree.(map[string]any)
	if !ok {
		return nil
	}
	names := []string{"node_id", "node_type", "run_when", "error_code", "executed_at"}
	identity := make(map[string]any, len(names))
	for _, name := range names {
		if value, present := fields[name]; present {
			identity[name] = value
		}
	}
	return identity
}
