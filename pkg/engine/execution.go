package engine

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
)

type executionState struct {
	allOutputs      map[string]map[string]interface{}
	remainingInputs map[node.AnyNode]int
	executedCount   int
	result          *node.FlowExecutionResult
	startTime       time.Time
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
		next := engine.findNodeWithoutInput(state.remainingInputs)

		if next == nil {
			return engine.finalizeExecution(state)
		}

		if err := engine.runNode(next, state); err != nil {
			state.result.Error = err
			state.result.DurationMS = time.Since(state.startTime).Milliseconds()
			return err
		}

		state.executedCount++
		engine.propagateNodeOutputs(next, state)
		engine.markNodeComplete(next, state)
	}
}

func (engine *FlowEngine) runNode(n node.AnyNode, state *executionState) error {
	nodeID := n.GetID()
	nodeType := n.GetType()

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
		return err
	}

	inputs := engine.assembleInputs(n, state.allOutputs)

	log.Debug().
		Str("flowName", engine.flow.Name).
		Str("nodeID", nodeID).
		Str("nodeType", string(nodeType)).
		Any("inputs", inputs).
		Msg("Assembled inputs for node")

	if engine.beforeExecution != nil {
		engine.beforeExecution(n)
	}

	ctx := node.ExecutionContext{
		Inputs:     inputs,
		AllOutputs: state.allOutputs,
	}

	startTime := time.Now()
	outputs, err := n.Execute(ctx)
	duration := time.Since(startTime)

	record := node.ExecutionResult{
		NodeID:     nodeID,
		Inputs:     inputs,
		Outputs:    outputs,
		Error:      err,
		ExecutedAt: time.Now(),
	}
	state.result.ExecutionResults[n.GetID()] = record

	if err != nil {
		log.Error().
			Str("flowName", engine.flow.Name).
			Str("nodeID", nodeID).
			Str("nodeType", string(nodeType)).
			Err(err).
			Int64("nodeDurationMS", duration.Milliseconds()).
			Msg("Node execution failed")
	} else {
		log.Info().
			Str("flowName", engine.flow.Name).
			Str("nodeID", nodeID).
			Str("nodeType", string(nodeType)).
			Int64("nodeDurationMS", duration.Milliseconds()).
			Any("outputs", outputs).
			Msg("Node executed successfully")
	}

	if engine.afterExecution != nil {
		engine.afterExecution(n, record)
	}

	return err
}

func (engine *FlowEngine) propagateNodeOutputs(n node.AnyNode, state *executionState) {
	outputs := state.result.ExecutionResults[n.GetID()].Outputs
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
