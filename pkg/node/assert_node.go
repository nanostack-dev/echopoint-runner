package node

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// AssertData configures an assert node. Target is the value to assert against;
// when empty the node asserts over the flow's initial inputs.
type AssertData struct {
	// Target is the value the assertions/extractors run against. It may carry
	// {{template}} or {{{raw}}} references that resolve to inputs/flow data. When
	// nil/empty the node falls back to asserting over the flow's initial inputs
	// (ctx.FlowInputs).
	//
	// To assert over an UPSTREAM NODE's output, set an explicit target that
	// references it (e.g. "{{{create-user.payload}}}"); InputSchema surfaces that
	// ref so the engine populates ctx.Inputs and the template resolves to the
	// upstream value. An OMITTED target asserts over the flow's initial inputs
	// (ctx.FlowInputs) — not over upstream node outputs.
	Target any `json:"target,omitempty"`
}

// AssertNode validates upstream or derived data with the standard assertion list.
// Unlike RequestNode it runs assertions over an in-memory value rather than an
// HTTP response, making it a first-class way to verify data produced by earlier
// nodes (transforms, modules, branches) in an API-flow test.
//
// To assert over an UPSTREAM NODE's output, set an explicit target referencing it
// (e.g. "{{{node.output}}}"); the omitted-target default instead asserts over the
// flow's initial inputs.
type AssertNode struct {
	BaseNode

	Data AssertData `json:"data"`

	// dynamic resolves {{$name}} variables; set per execution, not serialized.
	dynamic DynamicResolver
}

// AsAssertNode safely casts an AnyNode to an AssertNode.
// Returns the AssertNode and true if the cast succeeds, nil and false otherwise.
func AsAssertNode(node AnyNode) (*AssertNode, bool) {
	assertNode, ok := node.(*AssertNode)
	return assertNode, ok
}

// MustAsAssertNode casts an AnyNode to an AssertNode, panicking if it fails.
// Use this when you're certain the node is an AssertNode.
func MustAsAssertNode(node AnyNode) *AssertNode {
	assertNode, ok := AsAssertNode(node)
	if !ok {
		panic("expected AssertNode but got different type")
	}
	return assertNode
}

// GetData returns the node's typed data.
func (n *AssertNode) GetData() AssertData {
	return n.Data
}

// InputSchema infers inputs from template variables referenced in the target.
func (n *AssertNode) InputSchema() []string {
	return (&SchemaInference{}).ExtractTemplateVariables(n.Data.Target)
}

// OutputSchema returns the names of the declared output extractors.
func (n *AssertNode) OutputSchema() []string {
	return (&SchemaInference{}).InferRequestNodeOutputSchema(n.GetOutputs())
}

// Execute resolves the target value, runs every assertion against it, then
// extracts declared outputs. Assertions stop at the first failure (which is
// recorded) and the node returns a failed result with the matching error.
func (n *AssertNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()
	n.dynamic = ctx.DynamicVars

	log.Debug().
		Str("nodeID", n.GetID()).
		Any("inputs", ctx.Inputs).
		Msg("Starting assert node execution")

	target := n.resolveTarget(ctx)
	valueCtx := newValueResponseContext(target)

	assertionResults, assertErr := runNodeAssertions(n.GetID(), n.GetAssertions(), valueCtx)
	if assertErr != nil {
		return n.createErrorResult(ctx.Inputs, assertionResults, assertErr, time.Since(startTime)), assertErr
	}

	outputs, err := extractNodeOutputs(n.GetID(), n.GetOutputs(), valueCtx)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, assertionResults, err, time.Since(startTime)), err
	}

	result := &AssertExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeAssert,
			Inputs:      ctx.Inputs,
			Outputs:     outputs,
			ExecutedAt:  time.Now(),
		},
		AssertionResults: assertionResults,
		DurationMs:       time.Since(startTime).Milliseconds(),
	}

	log.Info().
		Str("nodeID", n.GetID()).
		Int("assertionCount", len(assertionResults)).
		Int("outputCount", len(outputs)).
		Int64("durationMs", result.DurationMs).
		Msg("Assert node executed successfully")

	return result, nil
}

// resolveTarget produces the value the assertions run against. When a target is
// configured it is template-resolved against the node inputs; otherwise the node
// asserts over the flow's initial inputs (ctx.FlowInputs) so jsonPath extractors
// can reach them. The engine only populates ctx.Inputs from refs declared in
// InputSchema (derived solely from Target templates), so an omitted target leaves
// ctx.Inputs empty — ctx.FlowInputs is the real, engine-populated map to assert
// over by default.
func (n *AssertNode) resolveTarget(ctx ExecutionContext) any {
	if n.Data.Target == nil {
		return mapOf(ctx.FlowInputs)
	}
	if s, ok := n.Data.Target.(string); ok && s == "" {
		return mapOf(ctx.FlowInputs)
	}

	resolver := NewTemplateResolverWithDynamics(ctx.Inputs, n.dynamic)
	resolved, err := resolver.Resolve(n.Data.Target)
	if err != nil {
		log.Warn().
			Str("nodeID", n.GetID()).
			Err(err).
			Msg("Assert target template resolution failed; using raw target")
		return n.Data.Target
	}
	return resolved
}

// createErrorResult builds a failed AssertExecutionResult carrying the assertions
// evaluated so far.
func (n *AssertNode) createErrorResult(
	inputs map[string]any,
	assertionResults []AssertionResult,
	err error,
	duration time.Duration,
) AnyExecutionResult {
	errMsg := err.Error()
	errCode := "ASSERT_FAILED"

	return &AssertExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeAssert,
			Inputs:      inputs,
			Outputs:     nil,
			Error:       err,
			ErrorMsg:    &errMsg,
			ErrorCode:   &errCode,
			ExecutedAt:  time.Now(),
		},
		AssertionResults: assertionResults,
		DurationMs:       duration.Milliseconds(),
	}
}

// mapOf returns a shallow copy of m so the asserted value is decoupled from the
// live inputs map, defaulting to an empty map when nil.
func mapOf(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}

// valueResponseContext adapts an in-memory value to the extractors.ResponseContext
// surface so jsonPath/body/header/statusCode extractors run against derived data
// rather than an HTTP response. The shared concreteResponseContext panics on a nil
// *http.Response, so assert/branch/sse-style nodes use this lightweight stand-in.
type valueResponseContext struct {
	raw    []byte
	parsed any
}

func newValueResponseContext(value any) *valueResponseContext {
	raw, _ := json.Marshal(value)
	return &valueResponseContext{raw: raw, parsed: value}
}

func (c *valueResponseContext) HasCapability(capability string) bool {
	switch capability {
	case "body", "parsed_body":
		return true
	}
	return false
}

func (c *valueResponseContext) GetParsedBody() any      { return c.parsed }
func (c *valueResponseContext) GetRawBody() []byte      { return c.raw }
func (c *valueResponseContext) GetBody() io.Reader      { return bytes.NewReader(c.raw) }
func (c *valueResponseContext) GetStatus() int          { return 0 }
func (c *valueResponseContext) GetHeader(string) string { return "" }
func (c *valueResponseContext) Headers() http.Header    { return http.Header{} }
