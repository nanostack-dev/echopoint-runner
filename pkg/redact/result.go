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
		Outputs:     set.outputs(result.GetNodeID(), result.GetOutputs()),
		Error:       set.maskError(result.GetError()),
		ExecutedAt:  result.GetExecutedAt(),
	}
	return &maskedResult{BaseExecutionResult: base, encoded: set.encodeMasked(result, base)}
}

// outputs masks a node's outputs by value, then its withheld ones by name.
func (set *secretSet) outputs(nodeID string, values map[string]any) map[string]any {
	masked := set.mapValues(values)
	set.maskOutputsByName(nodeID, masked)
	return masked
}

// encodeMasked serializes the masked form of result. Any failure falls back to
// the masked base fields alone: losing the node-kind detail is the closed side
// of this seam, reporting the unmasked original is not.
func (set *secretSet) encodeMasked(result spi.AnyResult, base spi.BaseExecutionResult) json.RawMessage {
	if tree, err := jsonTree(result); err == nil {
		if encoded, marshalErr := json.Marshal(set.maskResultTree(tree)); marshalErr == nil {
			return encoded
		}
	}
	fallback, err := json.Marshal(base)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return fallback
}

// maskResultTree masks a node result's tree, keeping the fields the runner
// writes itself.
func (set *secretSet) maskResultTree(tree any) any {
	identity := identityFields(tree)
	calls := detachHTTPCalls(tree)
	masked := set.mask(tree)
	fields, ok := masked.(map[string]any)
	if !ok {
		return masked
	}
	for name, value := range identity {
		if _, kept := fields[name]; kept {
			fields[name] = value
		}
	}
	if calls != nil {
		fields["http_calls"] = set.stringsOnly().mask(calls)
	}
	return fields
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
// "1" would rewrite executed_at, a duration could equal a numeric secret, and
// the control plane could no longer decode it.
func identityFields(tree any) map[string]any {
	fields, ok := tree.(map[string]any)
	if !ok {
		return nil
	}
	names := []string{
		"node_id", "node_type", "run_when", "error_code", "executed_at",
		"duration_ms", "response_status_code", "attempts", "iterations", "event_count", "delay_ms",
	}
	identity := make(map[string]any, len(names))
	for _, name := range names {
		if value, present := fields[name]; present {
			identity[name] = value
		}
	}
	return identity
}

// detachHTTPCalls removes a node result's http_calls, whose numbers are
// durations, offsets, status codes and byte counts the runner writes itself.
func detachHTTPCalls(tree any) any {
	fields, ok := tree.(map[string]any)
	if !ok {
		return nil
	}
	calls := fields["http_calls"]
	delete(fields, "http_calls")
	return calls
}

// stringsOnly is set masking string leaves only, never a number.
func (set *secretSet) stringsOnly() *secretSet {
	copied := *set
	copied.learned = nil
	return &copied
}
