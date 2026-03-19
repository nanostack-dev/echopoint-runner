package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	configpkg "github.com/nanostack-dev/echopoint-runner/internal/config"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	flowpkg "github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/rs/zerolog/log"
)

type Runtime struct {
	config configpkg.Config
	client *controlplane.Client
	bootID uuid.UUID

	sem chan struct{}

	activeMu   sync.RWMutex
	active     map[uuid.UUID]*activeJob
	rejectedMu sync.RWMutex
	rejected   map[uuid.UUID]struct{}

	workers sync.WaitGroup
}

type activeJob struct {
	job       *controlplane.ClaimedJob
	startedAt time.Time
}

func New(config configpkg.Config) *Runtime {
	return &Runtime{
		config: config,
		client: controlplane.NewClient(controlplane.Config{
			BaseURL:        config.BaseURL,
			OrganizationID: config.OrganizationID,
			RunnerAPIKey:   config.RunnerAPIKey,
			RequestTimeout: config.RequestTimeout,
		}),
		bootID:   uuid.Must(uuid.NewV7()),
		sem:      make(chan struct{}, config.MaxParallelFlows),
		active:   make(map[uuid.UUID]*activeJob),
		rejected: make(map[uuid.UUID]struct{}),
	}
}

func (r *Runtime) BootID() uuid.UUID {
	return r.bootID
}

func (r *Runtime) Run(ctx context.Context) error {
	log.Info().
		Str("runner_id", r.config.RunnerID).
		Str("boot_id", r.bootID.String()).
		Int("max_parallel_flows", r.config.MaxParallelFlows).
		Msg("starting runner runtime")

	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- r.runHeartbeatLoop(ctx)
	}()

	claimErr := r.runClaimLoop(ctx)
	r.waitForWorkers()

	heartbeatErr := <-heartbeatDone
	if claimErr != nil && !errors.Is(claimErr, context.Canceled) {
		return claimErr
	}
	if heartbeatErr != nil && !errors.Is(heartbeatErr, context.Canceled) {
		return heartbeatErr
	}

	return nil
}

func (r *Runtime) runClaimLoop(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := r.acquireSlot(ctx); err != nil {
			return err
		}

		claimStartedAt := time.Now()
		log.Info().
			Str("runner_id", r.config.RunnerID).
			Str("boot_id", r.bootID.String()).
			Str("organization_id", r.config.OrganizationID).
			Int("max_parallel_flows", r.config.MaxParallelFlows).
			Msg("waiting for runner job via long poll")

		claimedJob, err := r.client.ClaimNext(ctx, controlplane.ClaimNextRequest{
			RunnerID:         r.config.RunnerID,
			BootID:           r.bootID,
			MaxParallelFlows: r.config.MaxParallelFlows,
		})
		if err != nil {
			r.releaseSlot()
			if errors.Is(err, controlplane.ErrNoJobAvailable) {
				log.Info().
					Dur("poll_duration", time.Since(claimStartedAt)).
					Dur("idle_backoff", r.config.IdleBackoff).
					Msg("runner long poll returned no job")
				if sleepErr := sleepContext(ctx, r.config.IdleBackoff); sleepErr != nil {
					return sleepErr
				}
				continue
			}

			log.Error().Err(err).Dur("poll_duration", time.Since(claimStartedAt)).Msg("failed to claim runner job")
			if sleepErr := sleepContext(ctx, r.config.ErrorBackoff); sleepErr != nil {
				return sleepErr
			}
			continue
		}

		startedAt := time.Now().UTC()
		jobState := &activeJob{
			job:       claimedJob,
			startedAt: startedAt,
		}
		r.storeActiveJob(jobState)
		log.Info().
			Str("job_id", claimedJob.JobID.String()).
			Str("execution_id", claimedJob.ExecutionID.String()).
			Str("flow_id", claimedJob.FlowID.String()).
			Dur("poll_duration", time.Since(claimStartedAt)).
			Time("lease_expires_at", claimedJob.LeaseExpiresAt).
			Msg("claimed runner job")

		r.workers.Add(1)
		go func() {
			defer r.workers.Done()
			r.executeClaimedJob(jobState)
		}()
	}
}

func (r *Runtime) executeClaimedJob(active *activeJob) {
	defer r.releaseSlot()
	defer r.removeActiveJob(active.job.JobID)
	reporter := newJobEventReporter(r.client, active.job, r.config.RunnerID, r.bootID, r.config.RequestTimeout)
	defer reporter.Close()

	flowDef, err := flowpkg.ParseFromJSON(active.job.FlowDefinition)
	if err != nil {
		r.completeWithFailure(active, reporter, fmt.Sprintf("parse flow definition: %v", err), nil)
		return
	}
	if reporterErr := reporter.FlowStarted(flowDef.Name, time.Now().UTC()); reporterErr != nil {
		log.Warn().Err(reporterErr).Str("job_id", active.job.JobID.String()).Msg("failed to enqueue flow.started event")
	}

	flowEngine, err := engine.NewFlowEngine(*flowDef, &engine.Options{
		BeforeExecution: reporter.BeforeExecution,
		AfterExecution:  reporter.AfterExecution,
	})
	if err != nil {
		r.completeWithFailure(active, reporter, fmt.Sprintf("build flow engine: %v", err), nil)
		return
	}

	inputs := make(map[string]interface{}, len(active.job.Environment)+len(flowDef.InitialInputs))
	for key, value := range flowDef.InitialInputs {
		inputs[key] = value
	}
	for key, value := range active.job.Environment {
		inputs[key] = value
	}

	result, execErr := flowEngine.Execute(inputs)
	if execErr != nil {
		errorMsg := execErr.Error()
		var errorCode *string
		if result != nil {
			errorCode = result.ErrorCode
			if result.ErrorMsg != nil && *result.ErrorMsg != "" {
				errorMsg = *result.ErrorMsg
			}
		}
		if flushErr := reporter.FlushWithRetry(context.Background()); flushErr != nil {
			log.Error().
				Err(flushErr).
				Str("job_id", active.job.JobID.String()).
				Msg("failed to flush runner progress before failed completion")
		}
		r.completeWithFailure(active, reporter, errorMsg, errorCode)
		return
	}

	payload, err := controlplane.FlowExecutionResultToPayload(result)
	if err != nil {
		r.completeWithFailure(active, reporter, fmt.Sprintf("encode execution result: %v", err), nil)
		return
	}
	if flushErr := reporter.FlushWithRetry(context.Background()); flushErr != nil {
		log.Error().
			Err(flushErr).
			Str("job_id", active.job.JobID.String()).
			Msg("failed to flush runner progress before completion")
	}
	if r.isRejected(active.job.JobID) {
		log.Warn().
			Str("job_id", active.job.JobID.String()).
			Msg("skipping completion for rejected runner job")
		return
	}

	completedAt := time.Now().UTC()
	if completeErr := r.completeJob(active.job.JobID, controlplane.CompleteJobRequest{
		RunnerID:          r.config.RunnerID,
		BootID:            r.bootID,
		Status:            "completed",
		StartedAt:         active.startedAt,
		CompletedAt:       completedAt,
		DurationMs:        completedAt.Sub(active.startedAt).Milliseconds(),
		Result:            &payload,
		LastEventSequence: reporter.LastSequencePtr(),
	}); completeErr != nil {
		log.Error().Err(completeErr).Str("job_id", active.job.JobID.String()).Msg("failed to complete runner job")
		return
	}

	log.Info().
		Str("job_id", active.job.JobID.String()).
		Str("execution_id", active.job.ExecutionID.String()).
		Int64("duration_ms", completedAt.Sub(active.startedAt).Milliseconds()).
		Msg("runner job completed")
}

func (r *Runtime) completeWithFailure(
	active *activeJob,
	reporter *jobEventReporter,
	errorMsg string,
	errorCode *string,
) {
	if r.isRejected(active.job.JobID) {
		log.Warn().
			Str("job_id", active.job.JobID.String()).
			Msg("skipping failure completion for rejected runner job")
		return
	}

	completedAt := time.Now().UTC()
	err := r.completeJob(active.job.JobID, controlplane.CompleteJobRequest{
		RunnerID:          r.config.RunnerID,
		BootID:            r.bootID,
		Status:            "failed",
		StartedAt:         active.startedAt,
		CompletedAt:       completedAt,
		DurationMs:        completedAt.Sub(active.startedAt).Milliseconds(),
		ErrorMessage:      &errorMsg,
		ErrorCode:         errorCode,
		LastEventSequence: reporter.LastSequencePtr(),
	})
	if err != nil {
		log.Error().Err(err).Str("job_id", active.job.JobID.String()).Msg("failed to report runner job failure")
		return
	}

	log.Error().
		Str("job_id", active.job.JobID.String()).
		Str("execution_id", active.job.ExecutionID.String()).
		Str("error_message", errorMsg).
		Msg("runner job failed")
}

func (r *Runtime) runHeartbeatLoop(ctx context.Context) error {
	ticker := time.NewTicker(r.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		jobIDs := r.activeJobIDs()
		if len(jobIDs) == 0 {
			continue
		}

		results, err := r.client.Heartbeat(ctx, controlplane.HeartbeatRequest{
			RunnerID:         r.config.RunnerID,
			BootID:           r.bootID,
			MaxParallelFlows: r.config.MaxParallelFlows,
			JobIDs:           jobIDs,
		})
		if err != nil {
			log.Error().Err(err).Msg("runner heartbeat failed")
			continue
		}

		for _, result := range results {
			if result.Status == "rejected" {
				log.Warn().Str("job_id", result.JobID.String()).Msg("runner heartbeat rejected claimed job")
				r.markJobRejected(result.JobID)
			}
		}
	}
}

func (r *Runtime) completeJob(jobID uuid.UUID, request controlplane.CompleteJobRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.config.RequestTimeout)
	defer cancel()

	return r.client.Complete(ctx, jobID, request)
}

func (r *Runtime) acquireSlot(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r.sem <- struct{}{}:
		return nil
	}
}

func (r *Runtime) releaseSlot() {
	select {
	case <-r.sem:
	default:
	}
}

func (r *Runtime) storeActiveJob(job *activeJob) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()

	r.active[job.job.JobID] = job
}

func (r *Runtime) markJobRejected(jobID uuid.UUID) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()

	delete(r.active, jobID)

	r.rejectedMu.Lock()
	defer r.rejectedMu.Unlock()

	r.rejected[jobID] = struct{}{}
}

func (r *Runtime) isRejected(jobID uuid.UUID) bool {
	r.rejectedMu.RLock()
	defer r.rejectedMu.RUnlock()

	_, ok := r.rejected[jobID]
	return ok
}

func (r *Runtime) removeActiveJob(jobID uuid.UUID) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()

	delete(r.active, jobID)

	r.rejectedMu.Lock()
	defer r.rejectedMu.Unlock()

	delete(r.rejected, jobID)
}

func (r *Runtime) activeJobIDs() []uuid.UUID {
	r.activeMu.RLock()
	defer r.activeMu.RUnlock()

	jobIDs := make([]uuid.UUID, 0, len(r.active))
	for jobID := range r.active {
		jobIDs = append(jobIDs, jobID)
	}

	return jobIDs
}

func (r *Runtime) waitForWorkers() {
	workersDone := make(chan struct{})
	go func() {
		defer close(workersDone)
		r.workers.Wait()
	}()

	if r.config.ShutdownGracePeriod <= 0 {
		<-workersDone
		return
	}

	timer := time.NewTimer(r.config.ShutdownGracePeriod)
	defer timer.Stop()

	select {
	case <-workersDone:
	case <-timer.C:
		log.Warn().Dur("shutdown_grace_period", r.config.ShutdownGracePeriod).Msg("shutdown grace period elapsed")
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
