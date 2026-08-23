package cloudworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/pkg/dynamicvars"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/rs/zerolog/log"
)

const (
	heartbeatInterval = 10 * time.Second
	requestTimeout    = 30 * time.Second
	cloudRunnerID     = "cloud-worker"
	timestampKey      = "timestamp"
	timeoutErrorCode  = "EXECUTION_TIMEOUT"
	timeoutErrorMsg   = "execution timed out"
)

// Config is the control-plane address and the credentials for one Cloud run.
type Config struct {
	BaseURL  string
	JobID    uuid.UUID
	JobToken string
}

// Run fetches the Cloud job payload, runs the flow, and reports progress,
// heartbeats, and the terminal result through the runner-facing API.
func Run(ctx context.Context, cfg Config) error {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return errors.New("cloud worker api base url is missing")
	}
	if cfg.JobID == uuid.Nil {
		return errors.New("cloud worker job id is missing")
	}
	if cfg.JobToken == "" {
		return errors.New("cloud worker job token is missing")
	}

	bootID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	client := controlplane.NewClient(controlplane.Config{
		BaseURL:        cfg.BaseURL,
		JobToken:       cfg.JobToken,
		RequestTimeout: requestTimeout,
	})

	payload, err := client.GetJobPayload(ctx, cfg.JobID)
	if err != nil {
		return fmt.Errorf("fetch cloud job payload: %w", err)
	}

	inputs := payload.RunnerInputs
	if inputs == nil {
		inputs = map[string]any{}
	}
	parsed, err := flow.ParseFromJSONWithOptions(payload.FlowSnapshot, flow.ParseOptions{
		AllowedInitialInputKeys: inputKeys(inputs),
	})
	if err != nil {
		return fmt.Errorf("parse cloud job snapshot: %w", err)
	}

	duration := max(time.Duration(payload.CloudDurationSeconds)*time.Second, time.Second)
	engineCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		heartbeatLoop(engineCtx, client, cfg.JobID, bootID)
	}()

	observer := &httpObserver{
		ctx:         ctx,
		client:      client,
		jobID:       cfg.JobID,
		bootID:      bootID,
		executionID: payload.ExecutionID,
	}
	startedAt := time.Now().UTC()
	flowResult, runErr := runner.Run(
		*parsed,
		inputs,
		runner.WithContext(engineCtx),
		runner.WithObserver(observer),
		runner.WithReferencedFlows(payload.ReferencedFlows),
		runner.WithDynamicVars(dynamicvars.New(payload.ExecutionID.String())),
	)
	cancel()
	<-heartbeatDone

	return completeJob(ctx, client, completion{
		jobID:        cfg.JobID,
		bootID:       bootID,
		startedAt:    startedAt,
		lastSequence: observer.lastSequence(),
		result:       flowResult,
		runErr:       runErr,
		engineErr:    engineCtx.Err(),
	})
}

func heartbeatLoop(
	ctx context.Context, client *controlplane.Client, jobID uuid.UUID, bootID uuid.UUID,
) {
	postHeartbeat(ctx, client, jobID, bootID)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			postHeartbeat(ctx, client, jobID, bootID)
		}
	}
}

func postHeartbeat(
	ctx context.Context, client *controlplane.Client, jobID uuid.UUID, bootID uuid.UUID,
) {
	if _, err := client.Heartbeat(ctx, controlplane.HeartbeatRequest{
		RunnerID:         cloudRunnerID,
		BootID:           bootID,
		MaxParallelFlows: 1,
		JobIDs:           []uuid.UUID{jobID},
	}); err != nil && ctx.Err() == nil {
		log.Warn().Str("job_id", jobID.String()).Err(err).Msg("cloud job heartbeat failed")
	}
}

type completion struct {
	jobID        uuid.UUID
	bootID       uuid.UUID
	startedAt    time.Time
	lastSequence int64
	result       *spi.FlowExecutionResult
	runErr       error
	engineErr    error
}

func completeJob(ctx context.Context, client *controlplane.Client, done completion) error {
	completedAt := time.Now().UTC()
	if completedAt.Before(done.startedAt) {
		completedAt = done.startedAt
	}
	lastSequence := done.lastSequence
	request := controlplane.CompleteJobRequest{
		RunnerID:          cloudRunnerID,
		BootID:            done.bootID,
		StartedAt:         done.startedAt,
		CompletedAt:       completedAt,
		DurationMs:        completedAt.Sub(done.startedAt).Milliseconds(),
		LastEventSequence: &lastSequence,
	}

	timedOut := errors.Is(done.engineErr, context.DeadlineExceeded) ||
		errors.Is(done.runErr, context.DeadlineExceeded)
	switch {
	case timedOut:
		message, code := timeoutErrorMsg, timeoutErrorCode
		request.Status = string(statusFailed)
		request.ErrorMessage = &message
		request.ErrorCode = &code
	case done.runErr != nil || (done.result != nil && !done.result.Success):
		message := failureMessage(done.result, done.runErr)
		request.Status = string(statusFailed)
		request.ErrorMessage = &message
		if done.result != nil && done.result.ErrorCode != nil {
			request.ErrorCode = done.result.ErrorCode
		}
	default:
		payload, err := controlplane.FlowExecutionResultToPayload(done.result)
		if err != nil {
			return err
		}
		request.Status = string(statusCompleted)
		request.Result = &payload
	}

	if err := client.Complete(ctx, done.jobID, request); err != nil {
		return fmt.Errorf("complete cloud job: %w", err)
	}
	return nil
}

type jobStatus string

const (
	statusCompleted jobStatus = "completed"
	statusFailed    jobStatus = "failed"
)

func failureMessage(result *spi.FlowExecutionResult, runErr error) string {
	if runErr != nil {
		return runErr.Error()
	}
	if result != nil && result.ErrorMsg != nil && *result.ErrorMsg != "" {
		return *result.ErrorMsg
	}
	return "flow execution failed"
}

type httpObserver struct {
	ctx         context.Context
	client      *controlplane.Client
	jobID       uuid.UUID
	bootID      uuid.UUID
	executionID uuid.UUID

	mu           sync.Mutex
	sequence     int64
	lastAccepted int64
}

func (o *httpObserver) lastSequence() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastAccepted
}

func (o *httpObserver) nextSequence() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sequence++
	return o.sequence
}

func (o *httpObserver) acceptSequence(accepted int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lastAccepted = max(o.lastAccepted, accepted)
}

func (o *httpObserver) FlowStarted(evt engine.FlowStartedEvent) {
	o.post(spi.EventFlowStarted, map[string]any{
		"execution_id": o.executionID.String(),
		"flowName":     evt.FlowName,
		timestampKey:   evt.StartedAt.UTC().Format(time.RFC3339),
	})
}

func (o *httpObserver) NodeStarted(evt engine.NodeStartedEvent) {
	o.post(spi.EventNodeStarted, map[string]any{
		"nodeId":      evt.NodeID,
		"displayName": evt.DisplayName,
		"nodeType":    string(evt.NodeType),
		timestampKey:  evt.StartedAt.UTC().Format(time.RFC3339),
	})
}

func (o *httpObserver) NodeFinished(evt engine.NodeFinishedEvent) {
	payload := map[string]any{
		"nodeId":      evt.NodeID,
		"displayName": evt.DisplayName,
		"duration":    evt.DurationMs,
		timestampKey:  evt.FinishedAt.UTC().Format(time.RFC3339),
	}
	if evt.Result != nil && evt.Result.GetError() != nil {
		payload["error"] = evt.Result.GetError().Error()
		payload["success"] = false
		if result := anyResultMap(evt.Result); result != nil {
			payload["result"] = result
		}
		o.post(spi.EventNodeFailed, payload)
		return
	}
	payload["success"] = true
	payload["result"] = anyResultMap(evt.Result)
	o.post(spi.EventNodeCompleted, payload)
}

func (o *httpObserver) FlowFinished(engine.FlowFinishedEvent) {}

// post reports one progress event. The accepted sequence comes from the
// control plane, so a dropped event cannot make completion unacceptable.
func (o *httpObserver) post(eventType spi.EventType, payload map[string]any) {
	response, err := o.client.SendJobEvents(o.ctx, o.jobID, controlplane.SendJobEventsRequest{
		RunnerID: cloudRunnerID,
		BootID:   o.bootID,
		Events: []controlplane.RunnerProgressEvent{
			{
				Sequence: o.nextSequence(),
				Type:     eventType,
				Payload:  payload,
			},
		},
	})
	if err != nil {
		log.Warn().
			Str("job_id", o.jobID.String()).
			Str("event_type", string(eventType)).
			Err(err).
			Msg("cloud job progress event rejected")
		return
	}
	o.acceptSequence(response.LastAcceptedSequence)
}

func inputKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func anyResultMap(result spi.AnyResult) map[string]any {
	if result == nil {
		return nil
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil
	}
	var encoded map[string]any
	if unmarshalErr := json.Unmarshal(raw, &encoded); unmarshalErr != nil {
		return nil
	}
	return encoded
}
