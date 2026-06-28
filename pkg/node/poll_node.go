package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
)

// Default poll budgets applied when the wire payload omits them.
const (
	defaultPollMaxAttempts = 10
	defaultPollIntervalMs  = 1000
)

// PollData configures a poll-until node. The body sub-flow is re-run on an
// interval until the node's exit-condition assertions (BaseNode.Assertions) all
// pass against an attempt's child final outputs, or the attempt/deadline budget
// is exhausted.
type PollData struct {
	// Body is an inline flow definition executed once per attempt. The exit
	// condition (the node's assertions) is evaluated against its FinalOutputs.
	Body json.RawMessage `json:"body"`
	// MaxAttempts is the maximum number of body executions. Defaults to
	// defaultPollMaxAttempts when <= 0.
	MaxAttempts int `json:"max_attempts"`
	// IntervalMs is the wait between attempts, in milliseconds. Defaults to
	// defaultPollIntervalMs when <= 0.
	IntervalMs int `json:"interval_ms"`
	// TimeoutMs, when > 0, caps the overall wall-clock budget for the whole poll
	// (layered on top of the execution context deadline).
	TimeoutMs int `json:"timeout_ms"`
}

// PollNode re-runs an inline body flow on an interval until an exit-condition
// assertion holds — the canonical async-API pattern (kick off a job, then poll
// "status == done" before continuing).
type PollNode struct {
	BaseNode

	Data PollData `json:"data"`
}

// AsPollNode safely casts an AnyNode to a PollNode.
// Returns the PollNode and true if the cast succeeds, nil and false otherwise.
func AsPollNode(node AnyNode) (*PollNode, bool) {
	pollNode, ok := node.(*PollNode)
	return pollNode, ok
}

// MustAsPollNode casts an AnyNode to a PollNode, panicking if it fails.
// Use this when you're certain the node is a PollNode.
func MustAsPollNode(node AnyNode) *PollNode {
	pollNode, ok := AsPollNode(node)
	if !ok {
		panic("expected PollNode but got different type")
	}
	return pollNode
}

func (n *PollNode) GetData() PollData {
	return n.Data
}

// InputSchema returns empty: the poll node feeds the parent flow inputs into the
// body sub-flow rather than declaring named upstream dependencies.
func (n *PollNode) InputSchema() []string {
	return []string{}
}

// OutputSchema exposes the keys the poll node produces on success.
func (n *PollNode) OutputSchema() []string {
	return []string{"attempts", "result"}
}

func (n *PollNode) maxAttempts() int {
	if n.Data.MaxAttempts <= 0 {
		return defaultPollMaxAttempts
	}
	return n.Data.MaxAttempts
}

func (n *PollNode) intervalMs() int {
	if n.Data.IntervalMs <= 0 {
		return defaultPollIntervalMs
	}
	return n.Data.IntervalMs
}

// Execute runs the body sub-flow up to max_attempts times, evaluating the
// exit-condition assertions against each attempt's child final outputs. It
// succeeds on the first attempt where all assertions pass; otherwise it waits
// interval_ms and retries until the attempt budget, timeout, or context deadline
// is exhausted.
func (n *PollNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()

	if ctx.ModuleExecutor == nil {
		err := errors.New("module executor unavailable")
		return n.errorResult(ctx.Inputs, err, "POLL_FAILED", 0, nil, startTime), err
	}
	if len(n.Data.Body) == 0 {
		err := errors.New("poll body is required")
		return n.errorResult(ctx.Inputs, err, "POLL_FAILED", 0, nil, startTime), err
	}
	assertions := n.GetAssertions()
	if len(assertions) == 0 {
		err := errors.New("poll requires at least one exit-condition assertion")
		return n.errorResult(ctx.Inputs, err, "POLL_FAILED", 0, nil, startTime), err
	}

	pollCtx := ctx.Context()
	if n.Data.TimeoutMs > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(pollCtx, time.Duration(n.Data.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	maxAttempts := n.maxAttempts()
	interval := time.Duration(n.intervalMs()) * time.Millisecond

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("maxAttempts", maxAttempts).
		Int("intervalMs", n.intervalMs()).
		Int("timeoutMs", n.Data.TimeoutMs).
		Int("assertions", len(assertions)).
		Msg("Starting poll node execution")

	var lastAssertionResults []AssertionResult
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Abort before each attempt if the execution context (or the derived
		// timeout) is already done.
		if err := pollCtx.Err(); err != nil {
			log.Warn().
				Str("nodeID", n.GetID()).
				Int("attempt", attempt).
				Err(err).
				Msg("Poll node cancelled before attempt")
			return n.errorResult(ctx.Inputs, err, "POLL_CANCELLED", attempt-1, lastAssertionResults, startTime), err
		}

		childInputs := n.buildChildInputs(ctx.FlowInputs, attempt)
		res, err := ctx.ModuleExecutor.ExecuteModule(ModuleExecutionRequest{
			FlowID:         n.GetID() + "#poll",
			FlowDefinition: n.Data.Body,
			Inputs:         childInputs,
		})
		if err != nil {
			pollErr := fmt.Errorf("poll body failed on attempt %d: %w", attempt, err)
			log.Error().
				Str("nodeID", n.GetID()).
				Int("attempt", attempt).
				Err(pollErr).
				Msg("Poll body execution failed")
			return n.errorResult(
				ctx.Inputs, pollErr, "POLL_BODY_FAILED", attempt, lastAssertionResults, startTime,
			), pollErr
		}

		rc := extractors.NewValueResponseContext(res.FinalOutputs)
		assertionResults, assertErr := EvaluateAssertions(n.GetAssertions(), rc)
		lastAssertionResults = assertionResults

		if assertErr == nil {
			return n.successResult(ctx.Inputs, res.FinalOutputs, attempt, assertionResults, startTime), nil
		}

		log.Debug().
			Str("nodeID", n.GetID()).
			Int("attempt", attempt).
			Err(assertErr).
			Msg("Poll exit condition not yet met")

		// Wait interval_ms before retrying, unless this was the final attempt.
		if attempt < maxAttempts {
			if waitErr := sleepCtx(pollCtx, interval); waitErr != nil {
				log.Warn().
					Str("nodeID", n.GetID()).
					Int("attempt", attempt).
					Err(waitErr).
					Msg("Poll node cancelled while waiting between attempts")
				return n.errorResult(
					ctx.Inputs, waitErr, "POLL_CANCELLED", attempt, lastAssertionResults, startTime,
				), waitErr
			}
		}
	}

	notMetErr := fmt.Errorf("poll condition not met after %d attempts", maxAttempts)
	log.Error().
		Str("nodeID", n.GetID()).
		Int("attempts", maxAttempts).
		Err(notMetErr).
		Msg("Poll node exhausted attempts without meeting exit condition")
	return n.errorResult(
		ctx.Inputs, notMetErr, "POLL_CONDITION_NOT_MET", maxAttempts, lastAssertionResults, startTime,
	), notMetErr
}

// successResult builds the result for a poll attempt that met its exit condition.
func (n *PollNode) successResult(
	inputs map[string]any,
	finalOutputs map[string]any,
	attempt int,
	assertionResults []AssertionResult,
	startedAt time.Time,
) *PollExecutionResult {
	result := &PollExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypePoll,
			Inputs:      inputs,
			Outputs: map[string]any{
				"attempts": attempt,
				"result":   cloneMap(finalOutputs),
			},
			AssertionResults: assertionResults,
			ExecutedAt:       time.Now(),
		},
		Attempts:   attempt,
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
	log.Info().
		Str("nodeID", n.GetID()).
		Int("attempts", attempt).
		Int64("durationMs", result.DurationMs).
		Msg("Poll node exit condition met")
	return result
}

// buildChildInputs clones the parent flow inputs and injects the current attempt
// number so the body can reference {{attempt}} (e.g. for logging or backoff).
func (n *PollNode) buildChildInputs(flowInputs map[string]any, attempt int) map[string]any {
	childInputs := make(map[string]any, len(flowInputs)+1)
	maps.Copy(childInputs, flowInputs)
	childInputs["attempt"] = attempt
	return childInputs
}

func (n *PollNode) errorResult(
	inputs map[string]any,
	err error,
	code string,
	attempts int,
	assertionResults []AssertionResult,
	startedAt time.Time,
) AnyExecutionResult {
	errMsg := err.Error()
	errCode := code
	return &PollExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:           n.GetID(),
			DisplayName:      n.GetDisplayName(),
			NodeType:         TypePoll,
			Inputs:           inputs,
			Outputs:          nil,
			Error:            err,
			ErrorMsg:         &errMsg,
			ErrorCode:        &errCode,
			AssertionResults: assertionResults,
			ExecutedAt:       time.Now(),
		},
		Attempts:   attempts,
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
}

// sleepCtx waits for d, aborting early (and returning the context error) if the
// context is cancelled or its deadline elapses first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
