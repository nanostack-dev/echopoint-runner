package engine

import (
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type executionState struct {
	allOutputs      map[string]map[string]any
	remainingInputs map[node.AnyNode]int
	executedCount   int
	result          *node.FlowExecutionResult
	startTime       time.Time
	mainFailed      bool
	// failedNodes / skippedNodes track node IDs by terminal state so a skipped
	// node's reason can name the upstream step that caused it.
	// firstFailedName is the display name of the earliest failure, used when a
	// skip has no specific missing-input culprit.
	failedNodes     map[string]bool
	skippedNodes    map[string]bool
	firstFailedName string
	// deadEdges records, per routing node (source ID), the set of successor IDs
	// the source routed AWAY from. A successor whose every predecessor edge is
	// dead (or whose predecessors were all skipped/failed) is itself skipped,
	// cascading the routing decision through the untaken subtree.
	deadEdges map[string]map[string]bool
}

type nodeRunResult struct {
	node   node.AnyNode
	result node.AnyExecutionResult
	err    error
}

func (engine *FlowEngine) executeNodes(
	initialInputs map[string]any,
	result *node.FlowExecutionResult,
	startTime time.Time,
) error {
	state := &executionState{
		allOutputs:      make(map[string]map[string]any),
		remainingInputs: make(map[node.AnyNode]int),
		executedCount:   0,
		result:          result,
		startTime:       startTime,
		failedNodes:     make(map[string]bool),
		skippedNodes:    make(map[string]bool),
		deadEdges:       make(map[string]map[string]bool),
	}

	state.allOutputs[""] = initialInputs

	log.Debug().
		Str("flowName", engine.flow.Name).
		Any("initialInputs", initialInputs).
		Msg("Initialized flow execution with initial inputs")

	maps.Copy(state.remainingInputs, engine.nodeEdgeInput)

	engine.runOnSuccessPhase(state)
	if state.mainFailed {
		// Downstream on_success nodes can never run now — record them as skipped
		// with a reason naming the step that blocked them, instead of dropping
		// them silently (or erroring as "unreachable").
		engine.skipBlockedOnSuccessNodes(state)
	}
	engine.runAlwaysPhase(state)

	return engine.finalizeExecution(state)
}

func (engine *FlowEngine) runOnSuccessPhase(state *executionState) {
	for {
		ready := engine.readyNodes(state.remainingInputs, node.RunWhenOnSuccess)
		if len(ready) == 0 {
			return
		}

		// Partition ready nodes into those that should run and those whose every
		// live predecessor edge was routed away by a branch (fully dead). Dead
		// nodes are skipped — which decrements THEIR successors — so the skip
		// cascades through the untaken subtree on subsequent iterations.
		toRun := make([]node.AnyNode, 0, len(ready))
		toSkip := make([]node.AnyNode, 0)
		for _, readyNode := range ready {
			if engine.isFullyDead(readyNode, state) {
				toSkip = append(toSkip, readyNode)
				continue
			}
			toRun = append(toRun, readyNode)
		}

		for _, deadNode := range toSkip {
			engine.recordSkippedNode(deadNode, state, true)
		}

		if len(toRun) == 0 {
			// Progress was still made when nodes were skipped; loop again to pick
			// up the cascade. Only stop when nothing is ready at all.
			if len(toSkip) > 0 {
				continue
			}
			return
		}

		completed := engine.runReadyNodes(toRun, state)
		if engine.recordOnSuccessResults(completed, state) {
			return
		}
	}
}

// isFullyDead reports whether a node can never run because routing/skip/failure
// eliminated all of its incoming paths. A node is fully dead when it has at
// least one predecessor AND every predecessor p satisfies: the edge p -> n was
// routed away (deadEdges), or p was skipped, or p failed. Root nodes (no
// predecessors) are never dead.
func (engine *FlowEngine) isFullyDead(n node.AnyNode, state *executionState) bool {
	predecessors := engine.nodeEdgeSource[n]
	if len(predecessors) == 0 {
		return false
	}
	nodeID := n.GetID()
	for _, predecessor := range predecessors {
		predID := predecessor.GetID()
		edgeDead := state.deadEdges[predID][nodeID]
		if edgeDead || state.skippedNodes[predID] || state.failedNodes[predID] {
			continue
		}
		return false
	}
	return true
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
			engine.markNodeFailed(nodeResult.node, state)
		} else {
			state.executedCount++
		}

		if resultWithOutputs := nodeResult.result; resultWithOutputs != nil {
			engine.propagateNodeOutputs(nodeResult.node, resultWithOutputs, state)
		}

		// A routing node (e.g. branch) selects a subset of its successors; record
		// the untaken successor edges as dead so their subtrees get skipped. This
		// runs before markNodeComplete so successor input-counts stay consistent.
		if nodeResult.err == nil {
			engine.recordRoutingDecision(nodeResult.node, nodeResult.result, state)
		}
	}

	for _, nodeResult := range completed {
		engine.markNodeComplete(nodeResult.node, state)
	}

	return mainPhaseFailed
}

// recordRoutingDecision inspects a completed node's result for spi.RoutingResult
// and, when present, marks every successor edge the node routed AWAY from as
// dead. The engine then skips successors whose every incoming path is dead. The
// node is still marked complete normally so all successors are decremented and
// the scheduler's input counts stay consistent.
func (engine *FlowEngine) recordRoutingDecision(
	n node.AnyNode,
	result node.AnyExecutionResult,
	state *executionState,
) {
	routing, ok := result.(spi.RoutingResult)
	if !ok {
		return
	}

	taken := make(map[string]bool)
	for _, target := range routing.RoutedTargets() {
		taken[target] = true
	}

	nodeID := n.GetID()
	for _, successor := range engine.nodeEdgeOutput[n] {
		successorID := successor.GetID()
		if taken[successorID] {
			continue
		}
		if state.deadEdges[nodeID] == nil {
			state.deadEdges[nodeID] = make(map[string]bool)
		}
		state.deadEdges[nodeID][successorID] = true
	}

	log.Debug().
		Str("flowName", engine.flow.Name).
		Str("nodeID", nodeID).
		Strs("routedTargets", routing.RoutedTargets()).
		Msg("Recorded routing decision; untaken successor edges marked dead")
}

func (engine *FlowEngine) recordAlwaysResults(completed []nodeRunResult, state *executionState) {
	for _, nodeResult := range completed {
		state.result.ExecutionResults[nodeResult.node.GetID()] = nodeResult.result
		if nodeResult.err == nil {
			state.executedCount++
			engine.propagateNodeOutputs(nodeResult.node, nodeResult.result, state)
		} else {
			if state.result.Error == nil {
				state.result.Error = nodeResult.err
			}
			engine.markNodeFailed(nodeResult.node, state)
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
			state.skippedNodes[nodeID] = true
			skipped := engine.createSkippedNodeResult(n, err, state)
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
		// Input validation failure is caused by the user's flow definition, not a
		// runner fault. Classify it as a UserError and log at debug rather than
		// tripping error alerts.
		userErr := spi.NewUserError("NODE_INPUT_VALIDATION_FAILED", err.Error(), err)
		log.Debug().
			Str("flowName", engine.flow.Name).
			Str("nodeID", nodeID).
			Str("nodeType", string(nodeType)).
			Err(err).
			Int64("durationMS", time.Since(state.startTime).Milliseconds()).
			Msg("Node execution failed: input validation error")
		return nil, userErr
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

	executor := chainMiddleware(evalExecutor(n), engine.middleware)
	result, err := executor(engine.buildExecutionContext(inputs, outputView))
	finishedAt := time.Now()
	if result != nil && !result.GetExecutedAt().IsZero() {
		finishedAt = result.GetExecutedAt()
	}
	durationMs := finishedAt.Sub(startedAt).Milliseconds()

	if err != nil {
		// Log by error kind: a UserError is a user-caused outcome (their target
		// endpoint erred, an assertion failed, invalid node input) already
		// reported back as a failed node result, so it logs at debug. Anything
		// else is treated as a genuine runner fault and logged at error.
		event := log.Error()
		if _, ok := spi.AsUserError(err); ok {
			event = log.Debug()
		}
		event.
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

// evalExecutor wraps a node's Execute so the engine-level assertion/output pass
// runs INSIDE the middleware chain. Wrapping it before chainMiddleware means
// retry middleware still re-runs the node when an assertion fails — the assertion
// failure is part of the executed unit, not a post-chain step.
func evalExecutor(n node.AnyNode) NodeExecutor {
	return func(ec node.ExecutionContext) (node.AnyExecutionResult, error) {
		res, execErr := n.Execute(ec)
		if execErr != nil || res == nil {
			return res, execErr
		}
		return applyAssertionsAndOutputs(n, res)
	}
}

// applyAssertionsAndOutputs runs the uniform, engine-level assertion + output
// pass over a node's successful result. It is the single place assertions and
// outputs are evaluated for every node kind: a result opts in by implementing
// node.AssertionContextProvider and exposing a ResponseContext. Results that do
// not implement it (delay, module) are returned unchanged.
//
// On an assertion failure the result is marked failed (AssertionResults captured,
// error surfaced) and the error is returned so the node fails — and so retry
// middleware, which wraps this call, can re-run the node. Output extraction and
// schema validation run only after all assertions pass; produced outputs are
// merged into the result.
func applyAssertionsAndOutputs(
	n node.AnyNode, res node.AnyExecutionResult,
) (node.AnyExecutionResult, error) {
	provider, ok := res.(node.AssertionContextProvider)
	if !ok {
		return res, nil
	}
	rc := provider.AssertionContext()
	if rc == nil {
		// No context (e.g. an error result built before the exchange completed).
		return res, nil
	}

	failer, _ := res.(interface {
		SetAssertionResults([]spi.AssertionResult)
		Fail(error, string)
		MergeOutputs(map[string]any)
	})

	assertionResults, assertErr := node.EvaluateAssertions(n.GetAssertions(), rc)
	if failer != nil {
		failer.SetAssertionResults(assertionResults)
	}
	if assertErr != nil {
		if failer != nil {
			failer.Fail(assertErr, "ASSERTION_FAILED")
		}
		return res, assertErr
	}

	produced, extractErr := node.ExtractOutputs(n.GetOutputs(), rc)
	if extractErr != nil {
		if failer != nil {
			failer.Fail(extractErr, "OUTPUT_EXTRACTION_FAILED")
		}
		return res, extractErr
	}
	if failer != nil {
		failer.MergeOutputs(produced)
	}

	if validateErr := node.ValidateOutputs(n.OutputSchema(), produced); validateErr != nil {
		if failer != nil {
			failer.Fail(validateErr, "OUTPUT_VALIDATION_FAILED")
		}
		return res, validateErr
	}

	return res, nil
}

func (engine *FlowEngine) propagateNodeOutputs(
	n node.AnyNode,
	result node.AnyExecutionResult,
	state *executionState,
) {
	outputs := result.GetOutputs()
	nodeID := n.GetID()
	nodeType := n.GetType()
	copiedOutputs := make(map[string]any, len(outputs))
	maps.Copy(copiedOutputs, outputs)

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

// buildExecutionContext assembles the per-node ExecutionContext, propagating the
// flow's context, module resolver/executor, and dynamic vars.
func (engine *FlowEngine) buildExecutionContext(
	inputs map[string]any,
	outputView node.OutputView,
) node.ExecutionContext {
	return node.ExecutionContext{
		Ctx:            engine.ctx,
		Inputs:         inputs,
		FlowInputs:     outputView.Node(""),
		AllOutputs:     outputView,
		ModuleResolver: engine.moduleResolver,
		ModuleExecutor: moduleExecutor{
			resolver:  engine.moduleResolver,
			callStack: engine.moduleCallStack,
			ctx:       engine.ctx,
		},
		DynamicVars: engine.dynamicVars,
	}
}

func (engine *FlowEngine) markNodeComplete(n node.AnyNode, state *executionState) {
	successors := engine.nodeEdgeOutput[n]
	for _, successor := range successors {
		state.remainingInputs[successor]--
	}
	delete(state.remainingInputs, n)
}

func (engine *FlowEngine) markNodeFailed(n node.AnyNode, state *executionState) {
	if len(state.failedNodes) == 0 {
		state.firstFailedName = engine.nodeDisplayName(n.GetID())
	}
	state.failedNodes[n.GetID()] = true
}

// skipBlockedOnSuccessNodes records a skipped result for every on_success node
// that never ran after a main-phase failure (still present in remainingInputs).
// It deliberately does NOT mark them complete: leaving them in remainingInputs
// preserves the always-phase frontier logic, so downstream cleanup nodes whose
// real upstream was skipped stay blocked (and get skipped) rather than running.
func (engine *FlowEngine) skipBlockedOnSuccessNodes(state *executionState) {
	for _, currentNode := range engine.flow.Nodes {
		if currentNode.GetRunWhen() != node.RunWhenOnSuccess {
			continue
		}
		if _, exists := state.remainingInputs[currentNode]; !exists {
			continue
		}
		engine.recordSkippedNode(currentNode, state, false)
	}
}

// recordSkippedNode builds the skipped result, stores it, emits a NodeFinished
// event (so SSE/persistence observe the skip), and tracks it. When cascade is
// true it also unblocks successors via markNodeComplete (used by the always
// cleanup phase); on_success skips pass cascade=false.
func (engine *FlowEngine) recordSkippedNode(
	n node.AnyNode,
	state *executionState,
	cascade bool,
) {
	startedAt := time.Now()
	result := engine.createSkippedNodeResult(n, nil, state)
	state.result.ExecutionResults[n.GetID()] = result
	state.skippedNodes[n.GetID()] = true
	engine.observer.NodeFinished(NodeFinishedEvent{
		NodeID:      n.GetID(),
		DisplayName: n.GetDisplayName(),
		NodeType:    n.GetType(),
		StartedAt:   startedAt,
		FinishedAt:  time.Now(),
		DurationMs:  0,
		Result:      result,
	})
	if cascade {
		engine.markNodeComplete(n, state)
	}
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
		engine.recordSkippedNode(currentNode, state, true)
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
