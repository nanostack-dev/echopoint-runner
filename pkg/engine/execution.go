package engine

import (
	"fmt"
	"sort"
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

	for {
		ready := engine.readyNodes(state.remainingInputs)
		if len(ready) == 0 {
			return engine.finalizeExecution(state)
		}

		completed := engine.runReadyNodes(ready, state)
		for _, nodeResult := range completed {
			state.result.ExecutionResults[nodeResult.node.GetID()] = nodeResult.result
			if nodeResult.err != nil {
				state.result.Error = nodeResult.err
				state.result.DurationMS = time.Since(state.startTime).Milliseconds()
				return nodeResult.err
			}

			state.executedCount++
			engine.propagateNodeOutputs(nodeResult.node, nodeResult.result, state)
		}

		for _, nodeResult := range completed {
			engine.markNodeComplete(nodeResult.node, state)
		}
	}
}

func (engine *FlowEngine) readyNodes(remainingInputs map[node.AnyNode]int) []node.AnyNode {
	ready := make([]node.AnyNode, 0, len(remainingInputs))
	if len(remainingInputs) == 1 {
		for nodeKey, inputCount := range remainingInputs {
			if inputCount == 0 {
				return append(ready, nodeKey)
			}
		}
		return ready
	}

	for nodeKey, inputCount := range remainingInputs {
		if inputCount == 0 {
			ready = append(ready, nodeKey)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		return ready[i].GetID() < ready[j].GetID()
	})

	return ready
}

func (engine *FlowEngine) runReadyNodes(
	ready []node.AnyNode,
	state *executionState,
) []nodeRunResult {
	if len(ready) == 1 {
		result, err := engine.runNode(ready[0], state)
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
		go func(index int, n node.AnyNode) {
			defer wg.Done()

			result, err := engine.runNode(n, state)
			results[index] = nodeRunResult{
				node:   n,
				result: result,
				err:    err,
			}
		}(i, readyNode)
	}

	wg.Wait()
	return results
}

func (engine *FlowEngine) runNode(
	n node.AnyNode,
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

	if err := engine.validateInputs(n, state.allOutputs); err != nil {
		log.Error().
			Str("flowName", engine.flow.Name).
			Str("nodeID", nodeID).
			Str("nodeType", string(nodeType)).
			Err(err).
			Int64("durationMS", time.Since(state.startTime).Milliseconds()).
			Msg("Node execution failed: input validation error")
		return nil, err
	}

	inputs := engine.assembleInputs(n, state.allOutputs)

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
		Inputs:     inputs,
		AllOutputs: state.allOutputs,
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

	state.allOutputs[nodeID] = outputs

	for key, value := range outputs {
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
