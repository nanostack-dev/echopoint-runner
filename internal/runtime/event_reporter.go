package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
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
	flowName       string
	runnerID       string
	bootID         uuid.UUID
	requestTimeout time.Duration

	mu           sync.Mutex
	nextSequence int64
	pending      []controlplane.RunnerProgressEvent
	pendingBytes int

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

func (r *jobEventReporter) FlowStarted(evt engine.FlowStartedEvent) {
	r.flowName = evt.FlowName
	if err := r.enqueue(string(spi.EventFlowStarted), flowStartedPayload{
		ExecutionID: r.executionID.String(),
		FlowName:    evt.FlowName,
		Timestamp:   evt.StartedAt.Format(time.RFC3339),
	}); err != nil {
		log.Warn().Err(err).Str("job_id", r.jobID.String()).Msg("failed to enqueue flow.started event")
	}
}

func (r *jobEventReporter) NodeStarted(evt engine.NodeStartedEvent) {
	if err := r.enqueue(string(spi.EventNodeStarted), nodeStartedPayload{
		NodeID:      evt.NodeID,
		DisplayName: evt.DisplayName,
		NodeType:    string(evt.NodeType),
		Timestamp:   evt.StartedAt.Format(time.RFC3339),
	}); err != nil {
		log.Warn().
			Err(err).
			Str("job_id", r.jobID.String()).
			Str("node_id", evt.NodeID).
			Msg("failed to enqueue node.started event")
	}
}

func (r *jobEventReporter) NodeFinished(evt engine.NodeFinishedEvent) {
	payload := nodeFinishedPayload{
		NodeID:      evt.NodeID,
		DisplayName: evt.DisplayName,
		Duration:    evt.DurationMs,
		Timestamp:   evt.FinishedAt.Format(time.RFC3339),
	}

	// Always ship the engine's full result (the source of truth) — for failures
	// too, so assertion results and response detail survive.
	payload.Result = evt.Result

	eventType := spi.EventNodeCompleted
	if evt.Result != nil && evt.Result.GetError() == nil {
		succeeded := true
		payload.Success = &succeeded
	} else {
		eventType = spi.EventNodeFailed
		if evt.Result != nil && evt.Result.GetError() != nil {
			payload.Error = evt.Result.GetError().Error()
		} else {
			payload.Error = "node execution failed"
		}
	}

	if err := r.enqueue(string(eventType), payload); err != nil {
		log.Warn().
			Err(err).
			Str("job_id", r.jobID.String()).
			Str("node_id", evt.NodeID).
			Msg("failed to enqueue node terminal event")
	}
}

func (r *jobEventReporter) FlowFinished(evt engine.FlowFinishedEvent) {
	if evt.Result == nil {
		return
	}
	_ = evt
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

func (r *jobEventReporter) enqueue(eventType string, payload any) error {
	r.mu.Lock()
	r.nextSequence++
	event := controlplane.RunnerProgressEvent{
		Sequence: r.nextSequence,
		Type:     spi.EventType(eventType),
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

func eventSize(event controlplane.RunnerProgressEvent) int {
	encoded, err := json.Marshal(event)
	if err != nil {
		return 0
	}

	return len(encoded)
}
