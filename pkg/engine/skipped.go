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
	outputView node.OutputView,
) node.AnyExecutionResult {
	missingInputs := engine.collectMissingInputs(n, outputView)
	sort.Strings(missingInputs)
	skipReason := "missing_inputs"
	errorMessage := fmt.Sprintf("skipped because required inputs were unavailable: %v", missingInputs)
	if len(missingInputs) == 0 {
		skipReason = "not_reachable_after_main_phase"
		errorMessage = "skipped because the node could not be reached after the main phase finished"
	}
	if validationErr != nil && validationErr.Error() != "" {
		errorMessage = validationErr.Error()
	}
	errorCode := "NODE_SKIPPED"

	base := node.BaseExecutionResult{
		NodeID:        n.GetID(),
		DisplayName:   n.GetDisplayName(),
		NodeType:      n.GetType(),
		RunWhen:       n.GetRunWhen(),
		Inputs:        map[string]interface{}{},
		Outputs:       map[string]interface{}{},
		ErrorCode:     &errorCode,
		ErrorMsg:      &errorMessage,
		SkipReason:    &skipReason,
		MissingInputs: missingInputs,
		ExecutedAt:    time.Now(),
	}

	switch n.GetType() {
	case node.TypeRequest:
		return &node.RequestExecutionResult{BaseExecutionResult: base}
	case node.TypeDelay:
		return &node.DelayExecutionResult{BaseExecutionResult: base}
	}

	panic(fmt.Sprintf("unsupported node type: %s", n.GetType()))
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
