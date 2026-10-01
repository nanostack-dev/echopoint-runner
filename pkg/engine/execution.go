package engine

import (
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type executionState struct {
	allOutputs      map[string]map[string]any
	remainingInputs map[node.AnyNode]int
	executedCount   int
	result          *spi.FlowExecutionResult
	startTime       time.Time
	// failedNodes / skippedNodes track node IDs by terminal state so a skipped
	// node's reason can name the upstream step that caused it.
	// firstFailedName is the display name of the earliest failure, used when a
	// skip has no specific missing-input culprit.
	failedNodes  map[string]bool
	skippedNodes map[string]bool
	// failureCause maps a failed node, and every node skipped because of it, to
	// the display name of the step that failed.
	failureCause map[string]string
	// deadEdges records, per routing node (source ID), the set of successor IDs
	// the source routed AWAY from. A successor whose every predecessor edge is
	// dead (or whose predecessors were all skipped/failed) is itself skipped,
	// cascading the routing decision through the untaken subtree.
	deadEdges map[string]map[string]bool
}

type nodeRunResult struct {
	node   node.AnyNode
	result spi.AnyResult
	err    error
}

// missingInputsError is how an always node's goroutine reports that its inputs
// were not there; the scheduler turns it into a skipped result.
type missingInputsError struct {
	err       error
	startedAt time.Time
}

func (e missingInputsError) Error() string { return e.err.Error() }

func (e missingInputsError) Unwrap() error { return e.err }

func (engine *FlowEngine) executeNodes(
	initialInputs map[string]any,
	result *spi.FlowExecutionResult,
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
		failureCause:    make(map[string]string),
		deadEdges:       make(map[string]map[string]bool),
	}

	state.allOutputs[""] = initialInputs

	log.Debug().
		Str("flowName", engine.flow.Name).
		Int("initialInputCount", len(initialInputs)).
		Msg("Initialized flow execution with initial inputs")

	maps.Copy(state.remainingInputs, engine.nodeEdgeInput)

	engine.runOnSuccessPhase(state)
	engine.runAlwaysPhase(state)

	return engine.finalizeExecution(state)
}

// runOnSuccessPhase starts each node the moment its own predecessors finish,
// so a slow node (a delay, a long webhook wait) holds only its own successors.
// A failure skips the nodes downstream of it; every other branch keeps going.
func (engine *FlowEngine) runOnSuccessPhase(state *executionState) {
	scheduler := newPhaseScheduler(engine, state)
	for {
		engine.startReadyOnSuccessNodes(scheduler, state)
		if scheduler.idle() {
			return
		}
		finished := scheduler.next()
		engine.recordOnSuccessResults([]nodeRunResult{finished}, state)
	}
}

// startReadyOnSuccessNodes starts every ready node, and skips the ones whose
// every live predecessor edge was routed away (fully dead) or whose inputs
// name a skipped node. A skip unblocks its successors, so it repeats until no
// node changes.
func (engine *FlowEngine) startReadyOnSuccessNodes(scheduler *phaseScheduler, state *executionState) {
	for {
		skipped := false
		for _, readyNode := range scheduler.ready(spi.RunWhenOnSuccess) {
			// A node is skipped (rather than run) when it is fully dead OR when one
			// of its data inputs references the output of a node that was skipped
			// via routing/dead-edge. The latter is a cross-arm diamond join: the
			// join is reachable from a taken arm but templates an output from a
			// routed-away arm. Running it would hard-fail validateInputs and error
			// the whole flow; skipping it instead yields a graceful
			// dependency_skipped outcome.
			if cause, blocked := engine.upstreamFailure(readyNode, state); blocked {
				state.failureCause[readyNode.GetID()] = cause
				engine.recordSkippedNode(readyNode, state, true)
				skipped = true
				continue
			}
			if engine.isFullyDead(readyNode, state) ||
				engine.inputsReferenceSkippedNode(readyNode, state) {
				engine.recordSkippedNode(readyNode, state, true)
				skipped = true
				continue
			}
			scheduler.start(readyNode)
		}
		if !skipped {
			return
		}
	}
}

// upstreamFailure reports the failed step that blocks n, when a predecessor
// failed or was skipped because of a failure.
func (engine *FlowEngine) upstreamFailure(n node.AnyNode, state *executionState) (string, bool) {
	for _, predecessor := range engine.nodeEdgeSource[n] {
		if cause, failed := state.failureCause[predecessor.GetID()]; failed {
			return cause, true
		}
	}
	return "", false
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

// inputsReferenceSkippedNode reports whether any of n's InputSchema data-refs
// point to a source node that was skipped (via routing/dead-edge). Such a source
// produced no output, so running n would hard-fail validateInputs and error the
// whole flow. We skip n instead, letting describeSkipCause classify it as
// dependency_skipped. Refs to initial inputs (empty source) and refs to nodes
// that produced output are ignored, so nodes with all inputs available still run.
func (engine *FlowEngine) inputsReferenceSkippedNode(n node.AnyNode, state *executionState) bool {
	for _, inputKey := range n.InputSchema() {
		sourceNodeID, _, err := parseDataRef(inputKey)
		if err != nil || sourceNodeID == "" {
			continue
		}
		if state.skippedNodes[sourceNodeID] {
			return true
		}
	}
	return false
}

// runAlwaysPhase runs the cleanup nodes once the main phase is done. An
// on_success node placed after an always node runs here too, once its own
// predecessors succeeded.
func (engine *FlowEngine) runAlwaysPhase(state *executionState) {
	scheduler := newPhaseScheduler(engine, state)
	for {
		engine.startReadyOnSuccessNodes(scheduler, state)
		for _, readyNode := range scheduler.ready(spi.RunWhenAlways) {
			scheduler.start(readyNode)
		}
		if scheduler.idle() {
			// Nothing ready and nothing running: unblock always nodes whose
			// predecessors all ended, or stop.
			if !engine.resolveBlockedAlwaysNodes(scheduler, state) {
				return
			}
			continue
		}
		finished := scheduler.next()
		if finished.node.GetRunWhen() == spi.RunWhenOnSuccess {
			engine.recordOnSuccessResults([]nodeRunResult{finished}, state)
			continue
		}
		engine.recordAlwaysResults([]nodeRunResult{engine.settleAlwaysSkip(finished, state)}, state)
	}
}

// settleAlwaysSkip turns an always node that ran without its inputs into a
// skipped result. It runs on the scheduler, never in a node's goroutine,
// because building the skip reason reads the execution state.
func (engine *FlowEngine) settleAlwaysSkip(finished nodeRunResult, state *executionState) nodeRunResult {
	var missing missingInputsError
	if !errors.As(finished.err, &missing) {
		return finished
	}
	nodeID := finished.node.GetID()
	state.skippedNodes[nodeID] = true
	skipped := engine.createSkippedNodeResult(finished.node, missing.err, state)
	engine.observer.NodeFinished(NodeFinishedEvent{
		NodeID:      nodeID,
		DisplayName: finished.node.GetDisplayName(),
		NodeType:    finished.node.GetType(),
		StartedAt:   missing.startedAt,
		FinishedAt:  time.Now(),
		DurationMs:  time.Since(missing.startedAt).Milliseconds(),
		Result:      skipped,
	})
	return nodeRunResult{node: finished.node, result: skipped}
}

func (engine *FlowEngine) recordOnSuccessResults(completed []nodeRunResult, state *executionState) {
	for _, nodeResult := range completed {
		state.result.ExecutionResults[nodeResult.node.GetID()] = nodeResult.result
		if nodeResult.err != nil {
			if state.result.Error == nil {
				state.result.Error = nodeResult.err
			}
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
}

// recordRoutingDecision inspects a completed node's result for spi.RoutingResult
// and, when present, marks every successor edge the node routed AWAY from as
// dead. The engine then skips successors whose every incoming path is dead. The
// node is still marked complete normally so all successors are decremented and
// the scheduler's input counts stay consistent.
func (engine *FlowEngine) recordRoutingDecision(
	n node.AnyNode,
	result spi.AnyResult,
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
		} else {
			if state.result.Error == nil {
				state.result.Error = nodeResult.err
			}
			engine.markNodeFailed(nodeResult.node, state)
		}
		if nodeResult.result != nil {
			engine.propagateNodeOutputs(nodeResult.node, nodeResult.result, state)
		}
	}

	for _, nodeResult := range completed {
		engine.markNodeComplete(nodeResult.node, state)
	}
}

// phaseScheduler starts nodes as their predecessors finish. Only the loop
// that owns it touches the execution state; a node's goroutine only runs the
// node and hands the result back on done.
type phaseScheduler struct {
	engine   *FlowEngine
	state    *executionState
	done     chan nodeRunResult
	inFlight map[node.AnyNode]bool
}

func newPhaseScheduler(engine *FlowEngine, state *executionState) *phaseScheduler {
	return &phaseScheduler{
		engine:   engine,
		state:    state,
		done:     make(chan nodeRunResult, len(engine.flow.Nodes)),
		inFlight: make(map[node.AnyNode]bool),
	}
}

// ready lists, in flow order, the nodes of this phase whose predecessors all
// finished and that are not running yet.
func (s *phaseScheduler) ready(phase spi.RunWhen) []node.AnyNode {
	ready := make([]node.AnyNode, 0)
	for _, candidate := range s.engine.flow.Nodes {
		inputCount, pending := s.state.remainingInputs[candidate]
		if pending && inputCount == 0 && candidate.GetRunWhen() == phase && !s.inFlight[candidate] {
			ready = append(ready, candidate)
		}
	}
	return ready
}

// start runs n in its own goroutine against the outputs of every node that has
// finished so far.
func (s *phaseScheduler) start(n node.AnyNode) {
	s.inFlight[n] = true
	outputView := node.NewOutputView(s.state.allOutputs)
	go func() {
		result, err := s.engine.runNode(n, outputView, s.state)
		s.done <- nodeRunResult{node: n, result: result, err: err}
	}()
}

func (s *phaseScheduler) idle() bool {
	return len(s.inFlight) == 0
}

// next blocks until a running node finishes.
func (s *phaseScheduler) next() nodeRunResult {
	finished := <-s.done
	delete(s.inFlight, finished.node)
	return finished
}

func (engine *FlowEngine) runNode(
	n node.AnyNode,
	outputView spi.OutputView,
	state *executionState,
) (spi.AnyResult, error) {
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
		if n.GetRunWhen() == spi.RunWhenAlways {
			return nil, missingInputsError{err: err, startedAt: startedAt}
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
		Int("inputCount", len(inputs)).
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
			Str("errorCode", spi.ErrorCode(err)).
			Str("error", spi.SafeErrorMessage(err)).
			Msg("Node execution failed")
	} else {
		log.Info().
			Str("flowName", engine.flow.Name).
			Str("nodeID", nodeID).
			Str("nodeType", string(nodeType)).
			Int("outputCount", len(result.GetOutputs())).
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
	return func(ec spi.ExecutionContext) (spi.AnyResult, error) {
		res, execErr := n.Execute(ec)
		if execErr != nil || res == nil {
			return res, execErr
		}
		return applyAssertionsAndOutputs(n, res, ec)
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
	n node.AnyNode, res spi.AnyResult, ec spi.ExecutionContext,
) (spi.AnyResult, error) {
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

	assertions, resolveErr := node.ResolveAssertions(ec, n.GetAssertions())
	if resolveErr != nil {
		if failer != nil {
			failer.Fail(resolveErr, "ASSERTION_FAILED")
		}
		return res, resolveErr
	}

	assertionResults, assertErr := node.EvaluateAssertions(assertions, rc)
	if failer != nil {
		failer.SetAssertionResults(assertionResults)
	}
	if assertErr != nil {
		if failer != nil {
			failer.MergeOutputs(extractAvailableOutputs(n.GetOutputs(), rc))
			failer.Fail(assertErr, "ASSERTION_FAILED")
		}
		// A failed or erroring assertion is a user-caused outcome — the target
		// endpoint's response did not match the flow's assertions — already
		// reported on the result above. Return it as a UserError so the engine's
		// node-failure log classifies it at debug rather than error; a bare error
		// here surfaces expected assertion failures as error-level "Node execution
		// failed" logs that trip app-error alerts.
		return res, spi.NewUserError("ASSERTION_FAILED", assertErr.Error(), nil)
	}

	produced, extractErr := node.ExtractOutputs(n.GetOutputs(), rc)
	if extractErr != nil {
		if failer != nil {
			failer.Fail(extractErr, "OUTPUT_EXTRACTION_FAILED")
		}
		// Output extraction against the target's response is user-caused, same as
		// an assertion failure: wrap as a UserError so it logs at debug.
		return res, spi.NewUserError("OUTPUT_EXTRACTION_FAILED", extractErr.Error(), nil)
	}
	if failer != nil {
		failer.MergeOutputs(produced)
	}

	// Validate the OutputSchema against the result's full output set after the
	// merge, not just the freshly-extracted map: nodes whose outputs are produced
	// in Execute (e.g. set-variable) rather than by extractors expose them on the
	// result, and ExtractOutputs returns nothing for them. For extractor nodes
	// (request) the merged set equals the produced set, so behavior is unchanged.
	effectiveOutputs := produced
	if outputs := res.GetOutputs(); len(outputs) > 0 {
		effectiveOutputs = outputs
	}
	if validateErr := node.ValidateOutputs(n.OutputSchema(), effectiveOutputs); validateErr != nil {
		if failer != nil {
			failer.Fail(validateErr, "OUTPUT_VALIDATION_FAILED")
		}
		// A missing required output is a user-caused outcome (the flow declared an
		// output its response did not yield): wrap as a UserError so it logs at
		// debug rather than error.
		return res, spi.NewUserError("OUTPUT_VALIDATION_FAILED", validateErr.Error(), nil)
	}

	return res, nil
}

// extractAvailableOutputs keeps every output a failed node's response still
// yields, so an always-run cleanup can reference the resource it created.
func extractAvailableOutputs(outputs []node.Output, rc extractors.ResponseContext) map[string]any {
	available := make(map[string]any, len(outputs))
	for _, output := range outputs {
		if value, err := output.Extractor.Extract(rc); err == nil {
			available[output.Name] = value
		}
	}
	return available
}

func (engine *FlowEngine) propagateNodeOutputs(
	n node.AnyNode,
	result spi.AnyResult,
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
	outputView spi.OutputView,
) spi.ExecutionContext {
	return spi.ExecutionContext{
		Ctx:            engine.ctx,
		Inputs:         inputs,
		FlowInputs:     outputView.Node(""),
		AllOutputs:     outputView,
		ModuleResolver: engine.moduleResolver,
		ModuleExecutor: moduleExecutor{
			resolver:     engine.moduleResolver,
			callStack:    engine.moduleCallStack,
			ctx:          engine.ctx,
			dynamicVars:  engine.dynamicVars,
			secretValues: engine.secretValues,
			secretHosts:  engine.secretHosts,
		},
		DynamicVars:  engine.dynamicVars,
		SecretValues: engine.secretValues,
		SecretHosts:  engine.secretHosts,
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
	state.failedNodes[n.GetID()] = true
	state.failureCause[n.GetID()] = engine.nodeDisplayName(n.GetID())
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

// resolveBlockedAlwaysNodes handles always nodes that are stuck: every one of
// their predecessors finished (ran, failed, or was skipped), yet the abort left
// their edge count above zero, so readyNodes never offers them. A blocked node
// whose required runtime inputs all exist still RUNS — a cleanup step must not
// lose its turn just because a sibling test step failed upstream. Only a node
// genuinely missing an input (its producer never ran) is skipped. Either outcome
// unblocks later cleanup joins, such as delete_product after a delete_* step.
func (engine *FlowEngine) resolveBlockedAlwaysNodes(scheduler *phaseScheduler, state *executionState) bool {
	outputView := node.NewOutputView(state.allOutputs)
	toRun := make([]node.AnyNode, 0)
	toSkip := make([]node.AnyNode, 0)
	for _, currentNode := range engine.flow.Nodes {
		if currentNode.GetRunWhen() != spi.RunWhenAlways {
			continue
		}
		if _, exists := state.remainingInputs[currentNode]; !exists {
			continue
		}
		if engine.hasRemainingAlwaysPredecessor(currentNode, state) {
			continue
		}
		if len(engine.collectMissingInputs(currentNode, outputView)) == 0 {
			toRun = append(toRun, currentNode)
		} else {
			toSkip = append(toSkip, currentNode)
		}
	}

	if len(toRun) == 0 && len(toSkip) == 0 {
		return false
	}

	for _, currentNode := range toSkip {
		engine.recordSkippedNode(currentNode, state, true)
	}
	for _, currentNode := range toRun {
		scheduler.start(currentNode)
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
		if predecessor.GetRunWhen() == spi.RunWhenAlways {
			return true
		}
	}
	return false
}
