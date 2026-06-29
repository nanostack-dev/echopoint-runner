package node

import (
	"maps"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
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

// Execute resolves the target value and returns an AssertExecutionResult that
// exposes it as a ResponseContext (via AssertionContext). It does NOT run
// assertions or extract outputs itself: the engine-level pass drives those
// uniformly for every node that implements AssertionContextProvider (see
// engine.applyAssertionsAndOutputs), filling AssertionResults, merging outputs,
// and flipping the result to failed on a miss. Keeping the node thin means a
// single shared seam owns assertion/output evaluation and retry behavior.
func (n *AssertNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()
	n.dynamic = ctx.DynamicVars

	log.Debug().
		Str("nodeID", n.GetID()).
		Any("inputs", ctx.Inputs).
		Msg("Starting assert node execution")

	target := n.resolveTarget(ctx)

	result := &AssertExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeAssert,
			Inputs:      ctx.Inputs,
			ExecutedAt:  time.Now(),
		},
		assertionCtx: extractors.NewValueResponseContext(target),
		DurationMs:   time.Since(startTime).Milliseconds(),
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Int64("durationMs", result.DurationMs).
		Msg("Assert node resolved target; engine pass will evaluate assertions")

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

// mapOf returns a shallow copy of m so the asserted value is decoupled from the
// live inputs map, defaulting to an empty map when nil.
func mapOf(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}
