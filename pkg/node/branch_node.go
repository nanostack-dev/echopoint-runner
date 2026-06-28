package node

import (
	"maps"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
)

// BranchCase pairs a condition with the successor node ID to route to when the
// condition holds. When evaluates against the resolved branch target value (an
// in-memory value, not an HTTP response).
type BranchCase struct {
	When   CompositeAssertion `json:"when"`
	Target string             `json:"target"`
}

// BranchData configures value-based routing.
//
//   - Target is an optional template; its resolved value is what the case
//     conditions test. When omitted (nil), the branch tests the map of the
//     node's resolved inputs (ctx.Inputs).
//   - Cases are evaluated in order; the first whose When passes selects its
//     Target as the routed successor.
//   - Default is an optional successor node ID used when no case matches.
type BranchData struct {
	Target  any          `json:"target,omitempty"`
	Cases   []BranchCase `json:"cases"`
	Default string       `json:"default,omitempty"`
}

// BranchNode routes execution down exactly one downstream path based on a
// condition evaluated over upstream data. Unlike RunWhen (which gates on
// success/failure), a branch performs value-based routing: the chosen
// successor runs and the others are skipped by the engine.
type BranchNode struct {
	BaseNode

	Data BranchData `json:"data"`

	// dynamic resolves {{$name}} variables; set per execution, not serialized.
	dynamic DynamicResolver
}

// AsBranchNode safely casts an AnyNode to a BranchNode.
func AsBranchNode(candidate AnyNode) (*BranchNode, bool) {
	branchNode, ok := candidate.(*BranchNode)
	return branchNode, ok
}

// MustAsBranchNode casts an AnyNode to a BranchNode, panicking if it fails.
func MustAsBranchNode(candidate AnyNode) *BranchNode {
	branchNode, ok := AsBranchNode(candidate)
	if !ok {
		panic("expected BranchNode but got different type")
	}
	return branchNode
}

// GetData returns the branch configuration.
func (n *BranchNode) GetData() BranchData {
	return n.Data
}

// InputSchema infers inputs from template variables referenced in the optional
// target template.
func (n *BranchNode) InputSchema() []string {
	si := &SchemaInference{}
	return si.ExtractTemplateVariables(n.Data.Target)
}

// OutputSchema exposes the routing decision keys this node produces.
func (n *BranchNode) OutputSchema() []string {
	return []string{"matched", "matchedIndex"}
}

// Execute evaluates the branch cases against the resolved target value and
// selects exactly one successor (or none). It never fails on a "no match"
// outcome — that is a valid routing decision recorded in the result.
func (n *BranchNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()
	n.dynamic = ctx.DynamicVars

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("caseCount", len(n.Data.Cases)).
		Msg("Starting branch node execution")

	target := n.resolveTarget(ctx.Inputs)
	vc := extractors.NewValueResponseContext(target)

	chosen := ""
	matchedIndex := -1
	for i := range n.Data.Cases {
		res := n.Data.Cases[i].When.Evaluate(vc)
		if res.Error != "" {
			log.Debug().
				Str("nodeID", n.GetID()).
				Int("caseIndex", i).
				Str("error", res.Error).
				Msg("Branch case condition errored; treating as non-match")
			continue
		}
		if res.Passed {
			chosen = n.Data.Cases[i].Target
			matchedIndex = i
			break
		}
	}

	if chosen == "" && n.Data.Default != "" {
		chosen = n.Data.Default
	}

	routed := []string{}
	if chosen != "" {
		routed = []string{chosen}
	}

	result := &BranchExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeBranch,
			Inputs:      ctx.Inputs,
			Outputs: map[string]any{
				"matched":      chosen,
				"matchedIndex": matchedIndex,
			},
			ExecutedAt: time.Now(),
		},
		MatchedTarget:   chosen,
		RoutedTargetIDs: routed,
		DurationMs:      time.Since(startTime).Milliseconds(),
	}

	log.Info().
		Str("nodeID", n.GetID()).
		Str("matched", chosen).
		Int("matchedIndex", matchedIndex).
		Int64("durationMs", result.DurationMs).
		Msg("Branch node executed successfully")

	return result, nil
}

// resolveTarget produces the value the branch conditions test. When no explicit
// target template is configured it defaults to a copy of the node's resolved
// inputs, so conditions can assert over upstream outputs directly.
func (n *BranchNode) resolveTarget(inputs map[string]any) any {
	if n.Data.Target == nil {
		copied := make(map[string]any, len(inputs))
		maps.Copy(copied, inputs)
		return copied
	}
	resolver := NewTemplateResolverWithDynamics(inputs, n.dynamic)
	resolved, err := resolver.Resolve(n.Data.Target)
	if err != nil {
		log.Debug().
			Str("nodeID", n.GetID()).
			Err(err).
			Msg("Branch target template resolution failed; using raw target")
		return n.Data.Target
	}
	return resolved
}
