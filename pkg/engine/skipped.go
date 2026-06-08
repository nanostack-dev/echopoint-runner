package engine

import (
	"fmt"
	"sort"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

func (engine *FlowEngine) createSkippedNodeResult(
	n node.AnyNode,
	validationErr error,
	state *executionState,
) node.AnyExecutionResult {
	skipReason, errorMessage, missingInputs := engine.describeSkipCause(n, state)
	if validationErr != nil && validationErr.Error() != "" && skipReason == skipReasonNotReachable {
		// Preserve a validator-supplied message when we have nothing better.
		errorMessage = validationErr.Error()
	}
	errorCode := "NODE_SKIPPED"

	base := node.BaseExecutionResult{
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
	skipReasonAbortedAfterFail  = "aborted_after_failure"
	skipReasonNotReachable      = "not_reachable_after_main_phase"
)

// describeSkipCause classifies why a node was skipped and produces a
// human-readable message that names the responsible upstream step. Returns the
// machine-readable reason code, the message, and the sorted missing input refs.
func (engine *FlowEngine) describeSkipCause(
	n node.AnyNode,
	state *executionState,
) (string, string, []string) {
	missingInputs := engine.collectMissingInputs(n, node.NewOutputView(state.allOutputs))
	sort.Strings(missingInputs)

	var failedDep, skippedDep string
	seen := make(map[string]bool)
	for _, inputRef := range missingInputs {
		sourceNodeID, _, err := parseDataRef(inputRef)
		if err != nil || sourceNodeID == "" || seen[sourceNodeID] {
			continue
		}
		seen[sourceNodeID] = true
		if state.failedNodes[sourceNodeID] && failedDep == "" {
			failedDep = engine.nodeDisplayName(sourceNodeID)
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
	case state.firstFailedName != "":
		return skipReasonAbortedAfterFail,
			fmt.Sprintf("Skipped because step %q failed earlier in the flow", state.firstFailedName),
			missingInputs
	default:
		return skipReasonNotReachable,
			"Skipped because the node could not be reached after the main phase finished",
			missingInputs
	}
}

func (engine *FlowEngine) nodeDisplayName(nodeID string) string {
	if n, ok := engine.nodeMap[nodeID]; ok {
		if name := n.GetDisplayName(); name != "" {
			return name
		}
	}
	return nodeID
}

func (engine *FlowEngine) collectMissingInputs(n node.AnyNode, outputView node.OutputView) []string {
	missing := []string{}
	for _, inputKey := range n.InputSchema() {
		sourceNodeID, outputKey, err := parseDataRef(inputKey)
		if err != nil {
			missing = append(missing, inputKey)
			continue
		}
		if !outputView.HasNode(sourceNodeID) {
			missing = append(missing, inputKey)
			continue
		}
		if _, exists := outputView.Get(sourceNodeID, outputKey); !exists {
			missing = append(missing, inputKey)
		}
	}
	return missing
}
