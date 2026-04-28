package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

type executionState struct {
	allOutputs      map[string]map[string]interface{}
	remainingInputs map[node.AnyNode]int
	executedCount   int
	result          *node.FlowExecutionResult
	startTime       time.Time
	mainFailed      bool
}

type nodeRunResult struct {
	node   node.AnyNode
	result node.AnyExecutionResult
	err    error
}

func (engine *FlowEngine) executeNodes(
	initialInputs map[string]interface{},
	result *node.FlowExecutionResult,
	startTime time.Time,
) error {
	state := &executionState{
		allOutputs:      make(map[string]map[string]interface{}),
		remainingInputs: make(map[node.AnyNode]int),
		executedCount:   0,
		result:          result,
		startTime:       startTime,
	}

	state.allOutputs[""] = initialInputs

	log.Debug().
		Str("flowName", engine.flow.Name).
		Any("initialInputs", initialInputs).
		Msg("Initialized flow execution with initial inputs")

	for k, v := range engine.nodeEdgeInput {
		state.remainingInputs[k] = v
	}

	engine.runOnSuccessPhase(state)
	engine.runAlwaysPhase(state)

	return engine.finalizeExecution(state)
}

func (engine *FlowEngine) runOnSuccessPhase(state *executionState) {
	for {
		ready := engine.readyNodes(state.remainingInputs, node.RunWhenOnSuccess)
		if len(ready) == 0 {
			return
		}

		completed := engine.runReadyNodes(ready, state)
		if engine.recordOnSuccessResults(completed, state) {
			return
		}
	}
}

func (engine *FlowEngine) runAlwaysPhase(state *executionState) {
	for {
		ready := engine.readyNodes(state.remainingInputs, node.RunWhenAlways)
		if len(ready) == 0 {
			if !engine.skipFrontierAlwaysNodes(state) {
				return
			}
			continue
		}

		completed := engine.runReadyNodes(ready, state)
		engine.recordAlwaysResults(completed, state)
	}
}

func (engine *FlowEngine) recordOnSuccessResults(completed []nodeRunResult, state *executionState) bool {
	mainPhaseFailed := false
	for _, nodeResult := range completed {
		state.result.ExecutionResults[nodeResult.node.GetID()] = nodeResult.result
		if nodeResult.err != nil {
			if state.result.Error == nil {
				state.result.Error = nodeResult.err
			}
			state.mainFailed = true
			mainPhaseFailed = true
		} else {
			state.executedCount++
		}

		if resultWithOutputs := nodeResult.result; resultWithOutputs != nil {
			engine.propagateNodeOutputs(nodeResult.node, resultWithOutputs, state)
		}
	}

	for _, nodeResult := range completed {
		engine.markNodeComplete(nodeResult.node, state)
	}

	return mainPhaseFailed
}

func (engine *FlowEngine) recordAlwaysResults(completed []nodeRunResult, state *executionState) {
	for _, nodeResult := range completed {
		state.result.ExecutionResults[nodeResult.node.GetID()] = nodeResult.result
		if nodeResult.err == nil {
			state.executedCount++
			engine.propagateNodeOutputs(nodeResult.node, nodeResult.result, state)
		} else if state.result.Error == nil {
			state.result.Error = nodeResult.err
		}
	}

	for _, nodeResult := range completed {
		engine.markNodeComplete(nodeResult.node, state)
	}
}

func (engine *FlowEngine) readyNodes(remainingInputs map[node.AnyNode]int, phase node.RunWhen) []node.AnyNode {
	ready := make([]node.AnyNode, 0, len(remainingInputs))
	if len(remainingInputs) == 1 {
		for nodeKey, inputCount := range remainingInputs {
			if inputCount == 0 && nodeKey.GetRunWhen() == phase {
				return append(ready, nodeKey)
			}
		}
		return ready
	}

	for _, nodeKey := range engine.flow.Nodes {
		inputCount, exists := remainingInputs[nodeKey]
		if exists && inputCount == 0 && nodeKey.GetRunWhen() == phase {
			ready = append(ready, nodeKey)
		}
	}

	return ready
}

func (engine *FlowEngine) runReadyNodes(
	ready []node.AnyNode,
	state *executionState,
) []nodeRunResult {
	views := make([]node.OutputView, len(ready))
	for i := range ready {
		views[i] = node.NewOutputView(state.allOutputs)
	}

	if len(ready) == 1 {
		result, err := engine.runNode(ready[0], views[0], state)
		return []nodeRunResult{{
			node:   ready[0],
			result: result,
			err:    err,
		}}
	}

	results := make([]nodeRunResult, len(ready))
	var wg sync.WaitGroup
	wg.Add(len(ready))

	for i, readyNode := range ready {
		go func(index int, n node.AnyNode, outputView node.OutputView) {
			defer wg.Done()

			result, err := engine.runNode(n, outputView, state)
			results[index] = nodeRunResult{
				node:   n,
				result: result,
				err:    err,
			}
		}(i, readyNode, views[i])
	}

	wg.Wait()
	return results
}

func (engine *FlowEngine) runNode(
	n node.AnyNode,
	outputView node.OutputView,
	state *executionState,
) (node.AnyExecutionResult, error) {
	nodeID := n.GetID()
	displayName := n.GetDisplayName()
	nodeType := n.GetType()
	startedAt := time.Now()

	log.Debug().
		Str("flowName", engine.flow.Name).
		Str("nodeID", nodeID).
		Str("nodeType", string(nodeType)).
		Msg("Preparing node execution")

	if err := engine.validateInputs(n, outputView); err != nil {
		if n.GetRunWhen() == node.RunWhenAlways {
			skipped := engine.createSkippedNodeResult(n, err, outputView)
			engine.observer.NodeFinished(NodeFinishedEvent{
				NodeID:      nodeID,
				DisplayName: displayName,
				NodeType:    nodeType,
				StartedAt:   startedAt,
				FinishedAt:  time.Now(),
				DurationMs:  time.Since(startedAt).Milliseconds(),
				Result:      skipped,
			})
			return skipped, nil
		}
		log.Error().
			Str("flowName", engine.flow.Name).
			Str("nodeID", nodeID).
			Str("nodeType", string(nodeType)).
			Err(err).
			Int64("durationMS", time.Since(state.startTime).Milliseconds()).
			Msg("Node execution failed: input validation error")
		return nil, err
	}

	inputs := engine.assembleInputs(n, outputView)

	log.Debug().
		Str("flowName", engine.flow.Name).
		Str("nodeID", nodeID).
		Str("nodeType", string(nodeType)).
		Any("inputs", inputs).
		Msg("Assembled inputs for node")

	engine.observer.NodeStarted(NodeStartedEvent{
		NodeID:      nodeID,
		DisplayName: displayName,
		NodeType:    nodeType,
		StartedAt:   startedAt,
	})

	ctx := node.ExecutionContext{
		Inputs:         inputs,
		FlowInputs:     outputView.Node(""),
		AllOutputs:     outputView,
		ModuleResolver: engine.moduleResolver,
		ModuleExecutor: moduleExecutor{resolver: engine.moduleResolver, callStack: engine.moduleCallStack},
	}

	result, err := n.Execute(ctx)
	finishedAt := time.Now()
	if result != nil && !result.GetExecutedAt().IsZero() {
		finishedAt = result.GetExecutedAt()
	}
	durationMs := finishedAt.Sub(startedAt).Milliseconds()

	if err != nil {
		log.Error().
			Str("flowName", engine.flow.Name).
			Str("nodeID", nodeID).
			Str("nodeType", string(nodeType)).
			Err(err).
			Msg("Node execution failed")
	} else {
		log.Info().
			Str("flowName", engine.flow.Name).
			Str("nodeID", nodeID).
			Str("nodeType", string(nodeType)).
			Any("outputs", result.GetOutputs()).
			Msg("Node executed successfully")
	}

	engine.observer.NodeFinished(NodeFinishedEvent{
		NodeID:      nodeID,
		DisplayName: displayName,
		NodeType:    nodeType,
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
		DurationMs:  durationMs,
		Result:      result,
	})

	return result, err
}

func (engine *FlowEngine) propagateNodeOutputs(
	n node.AnyNode,
	result node.AnyExecutionResult,
	state *executionState,
) {
	outputs := result.GetOutputs()
	nodeID := n.GetID()
	nodeType := n.GetType()
	copiedOutputs := make(map[string]interface{}, len(outputs))
	for key, value := range outputs {
		copiedOutputs[key] = value
	}

	state.allOutputs[nodeID] = copiedOutputs

	for key, value := range copiedOutputs {
		flatKey := fmt.Sprintf("%s.%s", nodeID, key)
		state.result.FinalOutputs[flatKey] = value
	}

	log.Debug().
		Str("flowName", engine.flow.Name).
		Str("nodeID", nodeID).
		Str("nodeType", string(nodeType)).
		Int("outputCount", len(outputs)).
		Msg("Node outputs stored")
}

func (engine *FlowEngine) markNodeComplete(n node.AnyNode, state *executionState) {
	successors := engine.nodeEdgeOutput[n]
	for _, successor := range successors {
		state.remainingInputs[successor]--
	}
	delete(state.remainingInputs, n)
}

func (engine *FlowEngine) finalizeExecution(state *executionState) error {
	if state.result.Error != nil {
		state.result.Success = false
		state.result.DurationMS = time.Since(state.startTime).Milliseconds()
		return state.result.Error
	}

	if len(state.remainingInputs) > 0 {
		state.result.Error = fmt.Errorf(
			"cycle detected or unreachable nodes: %d nodes not executed",
			len(state.remainingInputs),
		)
		state.result.DurationMS = time.Since(state.startTime).Milliseconds()
		log.Error().
			Str("flowName", engine.flow.Name).
			Int("unreachableNodeCount", len(state.remainingInputs)).
			Err(state.result.Error).
			Int64("durationMS", state.result.DurationMS).
			Msg("Flow execution failed: cycle or unreachable nodes detected")
		return state.result.Error
	}

	state.result.Success = true
	state.result.DurationMS = time.Since(state.startTime).Milliseconds()
	log.Info().
		Str("flowName", engine.flow.Name).
		Int("executedNodes", state.executedCount).
		Int64("durationMS", state.result.DurationMS).
		Msg("Flow execution completed successfully")
	return nil
}

// skipFrontierAlwaysNodes skips cleanup nodes that are blocked only by nodes from
// the already-aborted main phase. Skipping those frontier nodes can unblock later
// cleanup joins that still have all required runtime inputs, such as delete_product
// after an earlier delete_* step was itself skipped.
func (engine *FlowEngine) skipFrontierAlwaysNodes(state *executionState) bool {
	toSkip := make([]node.AnyNode, 0)
	for _, currentNode := range engine.flow.Nodes {
		if currentNode.GetRunWhen() != node.RunWhenAlways {
			continue
		}
		if _, exists := state.remainingInputs[currentNode]; !exists {
			continue
		}
		if engine.hasRemainingAlwaysPredecessor(currentNode, state) {
			continue
		}
		toSkip = append(toSkip, currentNode)
	}

	if len(toSkip) == 0 {
		return false
	}

	for _, currentNode := range toSkip {
		result := engine.createSkippedNodeResult(currentNode, nil, node.NewOutputView(state.allOutputs))
		state.result.ExecutionResults[currentNode.GetID()] = result
		engine.markNodeComplete(currentNode, state)
	}

	return true
}

func (engine *FlowEngine) hasRemainingAlwaysPredecessor(
	currentNode node.AnyNode,
	state *executionState,
) bool {
	for _, predecessor := range engine.nodeEdgeSource[currentNode] {
		if _, exists := state.remainingInputs[predecessor]; !exists {
			continue
		}
		if predecessor.GetRunWhen() == node.RunWhenAlways {
			return true
		}
	}
	return false
}
