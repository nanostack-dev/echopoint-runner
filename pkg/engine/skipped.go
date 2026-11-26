package engine

import (
	"fmt"
	"sort"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func (engine *FlowEngine) createSkippedNodeResult(
	n node.AnyNode,
	validationErr error,
	state *executionState,
) spi.AnyResult {
	skipReason, errorMessage, missingInputs := engine.describeSkipCause(n, state)
	if validationErr != nil && validationErr.Error() != "" && skipReason == skipReasonNotReachable {
		// Preserve a validator-supplied message when we have nothing better.
		errorMessage = validationErr.Error()
	}
	errorCode := "NODE_SKIPPED"

	base := spi.BaseExecutionResult{
		NodeID:        n.GetID(),
		DisplayName:   n.GetDisplayName(),
		NodeType:      n.GetType(),
		RunWhen:       n.GetRunWhen(),
		Inputs:        map[string]any{},
		Outputs:       map[string]any{},
		ErrorCode:     &errorCode,
		ErrorMsg:      &errorMessage,
		SkipReason:    &skipReason,
		MissingInputs: missingInputs,
		ExecutedAt:    time.Now(),
	}

	result, ok := node.NewSkippedResult(n.GetType(), base)
	if !ok {
		panic(fmt.Sprintf("unsupported node type: %s", n.GetType()))
	}
	return result
}

const (
	skipReasonDependencyFailed  = "dependency_failed"
	skipReasonDependencySkipped = "dependency_skipped"
	skipReasonMissingInputs     = "missing_inputs"
	skipReasonNotReachable      = "not_reachable_after_main_phase"
	skipReasonRoutedAway        = "routed_away_by_branch"
)

// describeSkipCause classifies why a node was skipped and produces a
// human-readable message that names the responsible upstream step. Returns the
// machine-readable reason code, the message, and the sorted missing input refs.
func (engine *FlowEngine) describeSkipCause(
	n node.AnyNode,
	state *executionState,
) (string, string, []string) {
	outputView := node.NewOutputView(state.allOutputs)
	missingInputs := engine.collectMissingInputs(n, outputView)
	sort.Strings(missingInputs)

	// A node skipped purely because a branch routed every incoming edge away
	// (with no failed/skipped dependency to blame and no missing runtime inputs)
	// is an intentional routing outcome, not an error — name the branch.
	if routedBy, ok := engine.routedAwayBy(n, state); ok {
		if engine.isFailureEdge(routedBy.id, n.GetID()) {
			return skipReasonRoutedAway,
				fmt.Sprintf("Skipped because step %q did not fail; this step runs only on its failure", routedBy.name),
				missingInputs
		}
		return skipReasonRoutedAway,
			fmt.Sprintf("Skipped because branch %q routed execution down a different path", routedBy.name),
			missingInputs
	}

	var failedDep, skippedDep string
	seen := make(map[string]bool)
	for _, inputRef := range missingInputs {
		// resolveDataRef keeps a path rooted at a flow input with that input,
		// so a missing field is not blamed on a skipped step of the same name.
		sourceNodeID, _, err := resolveDataRef(inputRef, outputView)
		if err != nil || sourceNodeID == "" || seen[sourceNodeID] {
			continue
		}
		seen[sourceNodeID] = true
		if cause, failed := state.failureCause[sourceNodeID]; failed && failedDep == "" {
			failedDep = cause
		} else if state.skippedNodes[sourceNodeID] && skippedDep == "" {
			skippedDep = engine.nodeDisplayName(sourceNodeID)
		}
	}

	switch {
	case failedDep != "":
		return skipReasonDependencyFailed,
			fmt.Sprintf("Skipped because step %q failed", failedDep),
			missingInputs
	case skippedDep != "":
		return skipReasonDependencySkipped,
			fmt.Sprintf("Skipped because step %q was skipped", skippedDep),
			missingInputs
	case len(missingInputs) > 0:
		return skipReasonMissingInputs,
			fmt.Sprintf("Skipped because required inputs were unavailable: %v", missingInputs),
			missingInputs
	case state.failureCause[n.GetID()] != "":
		return skipReasonDependencyFailed,
			fmt.Sprintf("Skipped because step %q failed", state.failureCause[n.GetID()]),
			missingInputs
	default:
		return skipReasonNotReachable,
			"Skipped because the node could not be reached after the main phase finished",
			missingInputs
	}
}

// routingSource names the predecessor whose dead edge explains a routed-away
// skip: a branch, or the source of a failure edge that did not fail.
type routingSource struct {
	id   string
	name string
}

// routedAwayBy reports the predecessor whose dead edge (a branch routed away
// from it, or a failure edge whose source did not fail) left n with no live
// incoming edge, when that is the reason n was skipped. It returns false when
// n has a failed or skipped predecessor (those are reported with a more
// specific reason) or when no predecessor edge is dead.
func (engine *FlowEngine) routedAwayBy(n node.AnyNode, state *executionState) (routingSource, bool) {
	predecessors := engine.nodeEdgeSource[n]
	if len(predecessors) == 0 {
		return routingSource{}, false
	}

	var routedBy routingSource
	for _, predecessor := range predecessors {
		predID := predecessor.GetID()
		switch {
		case state.failedNodes[predID]:
			// A failed dependency is a more specific, higher-priority reason.
			return routingSource{}, false
		case engine.edgeDead(predecessor, n, state):
			if routedBy.id == "" {
				routedBy = routingSource{id: predID, name: engine.nodeDisplayName(predID)}
			}
		case state.skippedNodes[predID]:
			// Skipped dependency: keep looking; a sibling dead edge may still
			// explain this skip, but a skipped dep alone is reported elsewhere.
		default:
			// A live predecessor that neither failed, was skipped, nor routed
			// away means this node was not eliminated by routing.
			return routingSource{}, false
		}
	}
	if routedBy.id == "" {
		return routingSource{}, false
	}
	return routedBy, true
}

func (engine *FlowEngine) nodeDisplayName(nodeID string) string {
	if n, ok := engine.nodeMap[nodeID]; ok {
		if name := n.GetDisplayName(); name != "" {
			return name
		}
	}
	return nodeID
}

func (engine *FlowEngine) collectMissingInputs(n node.AnyNode, outputView spi.OutputView) []string {
	missing := []string{}
	for _, inputKey := range n.InputSchema() {
		sourceNodeID, outputKey, err := resolveDataRef(inputKey, outputView)
		if err != nil {
			missing = append(missing, inputKey)
			continue
		}
		if !outputView.HasNode(sourceNodeID) {
			missing = append(missing, inputKey)
			continue
		}
		if _, exists := lookupOutput(outputView, sourceNodeID, outputKey); !exists {
			missing = append(missing, inputKey)
		}
	}
	return missing
}
