package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/pkg/executionevents"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/rs/zerolog/log"
)

const (
	reporterMaxBatchEvents = 16
	reporterMaxBatchBytes  = 32 * 1024
	reporterFlushInterval  = 150 * time.Millisecond
	reporterFlushRetries   = 3
	reporterRetryBackoff   = 200 * time.Millisecond
)

type jobEventReporter struct {
	client         *controlplane.Client
	jobID          uuid.UUID
	executionID    uuid.UUID
	runnerID       string
	bootID         uuid.UUID
	requestTimeout time.Duration

	mu             sync.Mutex
	nextSequence   int64
	pending        []controlplane.RunnerProgressEvent
	pendingBytes   int
	nodeStartTimes map[string]time.Time

	flushMu sync.Mutex
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func newJobEventReporter(
	client *controlplane.Client,
	job *controlplane.ClaimedJob,
	runnerID string,
	bootID uuid.UUID,
	requestTimeout time.Duration,
) *jobEventReporter {
	reporter := &jobEventReporter{
		client:         client,
		jobID:          job.JobID,
		executionID:    job.ExecutionID,
		runnerID:       runnerID,
		bootID:         bootID,
		requestTimeout: requestTimeout,
		nodeStartTimes: make(map[string]time.Time),
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}

	go reporter.run()
	return reporter
}

func (r *jobEventReporter) run() {
	defer close(r.doneCh)

	ticker := time.NewTicker(reporterFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), r.requestTimeout)
			if err := r.Flush(ctx); err != nil {
				log.Warn().Err(err).Str("job_id", r.jobID.String()).Msg("failed to flush runner progress batch")
			}
			cancel()
		}
	}
}

func (r *jobEventReporter) Close() {
	close(r.stopCh)
	<-r.doneCh
}

func (r *jobEventReporter) LastSequencePtr() *int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nextSequence == 0 {
		return nil
	}

	value := r.nextSequence
	return &value
}

func (r *jobEventReporter) FlowStarted(flowName string, at time.Time) error {
	return r.enqueue(string(executionevents.FlowStarted), map[string]interface{}{
		"execution_id": r.executionID.String(),
		"flowName":     flowName,
		"timestamp":    at.Format(time.RFC3339),
	})
}

func (r *jobEventReporter) BeforeExecution(n node.AnyNode) {
	startedAt := time.Now().UTC()

	r.mu.Lock()
	r.nodeStartTimes[n.GetID()] = startedAt
	r.mu.Unlock()

	if err := r.enqueue(string(executionevents.NodeStarted), map[string]interface{}{
		"nodeId":      n.GetID(),
		"displayName": n.GetDisplayName(),
		"nodeType":    string(n.GetType()),
		"timestamp":   startedAt.Format(time.RFC3339),
	}); err != nil {
		log.Warn().
			Err(err).
			Str("job_id", r.jobID.String()).
			Str("node_id", n.GetID()).
			Msg("failed to enqueue node.started event")
	}
}

func (r *jobEventReporter) AfterExecution(n node.AnyNode, result node.AnyExecutionResult) {
	completedAt := time.Now().UTC()
	durationMs := r.nodeDuration(n.GetID(), completedAt)

	if result != nil && result.GetExecutedAt().After(time.Time{}) {
		completedAt = result.GetExecutedAt().UTC()
		durationMs = r.nodeDuration(n.GetID(), completedAt)
	}

	payload := map[string]interface{}{
		"nodeId":      n.GetID(),
		"displayName": n.GetDisplayName(),
		"duration":    durationMs,
		"timestamp":   completedAt.Format(time.RFC3339),
	}

	eventType := executionevents.NodeCompleted
	if result != nil && result.GetError() == nil {
		payload["success"] = true
		payload["result"] = executionResultToEventPayload(result)
	} else {
		eventType = executionevents.NodeFailed
		if result != nil && result.GetError() != nil {
			payload["error"] = result.GetError().Error()
		} else {
			payload["error"] = "node execution failed"
		}
	}

	if err := r.enqueue(string(eventType), payload); err != nil {
		log.Warn().
			Err(err).
			Str("job_id", r.jobID.String()).
			Str("node_id", n.GetID()).
			Msg("failed to enqueue node terminal event")
	}
}

func (r *jobEventReporter) FlushWithRetry(ctx context.Context) error {
	var lastErr error
	for attempt := range reporterFlushRetries {
		flushErr := r.Flush(ctx)
		if flushErr == nil {
			return nil
		}
		lastErr = flushErr

		if attempt == reporterFlushRetries-1 {
			break
		}
		if sleepErr := sleepContext(ctx, reporterRetryBackoff); sleepErr != nil {
			return lastErr
		}
	}

	return lastErr
}

func (r *jobEventReporter) Flush(ctx context.Context) error {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	batch, batchBytes := r.drainPending()
	if len(batch) == 0 {
		return nil
	}

	response, err := r.client.SendJobEvents(ctx, r.jobID, controlplane.SendJobEventsRequest{
		RunnerID: r.runnerID,
		BootID:   r.bootID,
		Events:   batch,
	})
	if err != nil {
		r.prependPending(batch, batchBytes)
		return err
	}

	lastAccepted := response.LastAcceptedSequence
	if lastAccepted >= batch[len(batch)-1].Sequence {
		return nil
	}

	remaining := make([]controlplane.RunnerProgressEvent, 0, len(batch))
	remainingBytes := 0
	for _, event := range batch {
		if event.Sequence <= lastAccepted {
			continue
		}
		remaining = append(remaining, event)
		remainingBytes += eventSize(event)
	}
	r.prependPending(remaining, remainingBytes)
	return nil
}

func (r *jobEventReporter) enqueue(eventType string, payload map[string]interface{}) error {
	r.mu.Lock()
	r.nextSequence++
	event := controlplane.RunnerProgressEvent{
		Sequence: r.nextSequence,
		Type:     executionevents.Type(eventType),
		Payload:  payload,
	}
	r.pending = append(r.pending, event)
	r.pendingBytes += eventSize(event)
	shouldFlush := len(r.pending) >= reporterMaxBatchEvents || r.pendingBytes >= reporterMaxBatchBytes
	r.mu.Unlock()

	if !shouldFlush {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.requestTimeout)
	defer cancel()
	return r.Flush(ctx)
}

func (r *jobEventReporter) drainPending() ([]controlplane.RunnerProgressEvent, int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.pending) == 0 {
		return nil, 0
	}

	batch := append([]controlplane.RunnerProgressEvent(nil), r.pending...)
	bytes := r.pendingBytes
	r.pending = nil
	r.pendingBytes = 0
	return batch, bytes
}

func (r *jobEventReporter) prependPending(events []controlplane.RunnerProgressEvent, bytes int) {
	if len(events) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(events, r.pending...)
	r.pendingBytes += bytes
}

func (r *jobEventReporter) nodeDuration(nodeID string, completedAt time.Time) int64 {
	r.mu.Lock()
	startedAt := r.nodeStartTimes[nodeID]
	delete(r.nodeStartTimes, nodeID)
	r.mu.Unlock()
	if startedAt.IsZero() {
		return 0
	}

	return completedAt.Sub(startedAt).Milliseconds()
}

func eventSize(event controlplane.RunnerProgressEvent) int {
	encoded, err := json.Marshal(event)
	if err != nil {
		return 0
	}

	return len(encoded)
}

func executionResultToEventPayload(result node.AnyExecutionResult) map[string]interface{} {
	if result == nil {
		return nil
	}

	payload := map[string]interface{}{
		"node_id":      result.GetNodeID(),
		"display_name": result.GetDisplayName(),
		"node_type":    string(result.GetNodeType()),
		"outputs":      result.GetOutputs(),
	}

	if requestResult, ok := node.AsRequestExecutionResult(result); ok {
		payload["request"] = map[string]interface{}{
			"method":  requestResult.RequestMethod,
			"url":     requestResult.RequestURL,
			"headers": requestResult.RequestHeaders,
		}
		payload["response"] = map[string]interface{}{
			"status_code": requestResult.ResponseStatusCode,
			"headers":     requestResult.ResponseHeaders,
		}
		payload["duration_ms"] = requestResult.DurationMs
	} else if delayResult, isDelayResult := node.AsDelayExecutionResult(result); isDelayResult {
		payload["delay_ms"] = delayResult.DelayMs
	}

	return payload
}
