// Command cloudworker serves Cloud job assigns inside a Cloudflare container
// and runs one runner process per assigned job.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/pkg/cloudworker"
	"github.com/rs/zerolog/log"
)

const (
	listenAddr        = ":8080"
	runCommand        = "run"
	envAPIBaseURL     = "ECHOPOINT_API_BASE_URL"
	envJobID          = "CLOUD_JOB_ID"
	envJobToken       = "CLOUD_JOB_TOKEN"
	envMaxJobs        = "CLOUD_WORKER_MAX_JOBS"
	defaultMaxJobs    = 1
	readHeaderTimeout = 5 * time.Second
	// The platform sends SIGTERM and waits 15 minutes before SIGKILL. Draining
	// stops one minute short of that, so a job that ignores its own deadline
	// does not turn a rollout into a kill.
	drainTimeout = 14 * time.Minute
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == runCommand {
		if err := runAssignedJob(); err != nil {
			log.Error().Err(err).Msg("cloud job failed")
			os.Exit(1)
		}
		return
	}
	if err := serve(); err != nil {
		log.Fatal().Err(err).Msg("cloud worker stopped")
	}
}

func serve() error {
	capacity := maxJobs()
	worker := cloudworker.NewServer(launchRunnerProcess, capacity)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           worker.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		log.Info().Str("addr", listenAddr).Int("capacity", capacity).Msg("cloud worker listening")
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop()
		return drain(worker, server)
	}
}

// drain refuses new assigns, waits for the jobs this container already holds,
// and only then closes the server. A killed job is a failed customer run that
// never runs again, so the wait is worth the deploy latency.
func drain(worker *cloudworker.Server, server *http.Server) error {
	log.Info().Int("running", worker.Running()).Msg("draining cloud worker")
	worker.StopAccepting()

	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	if err := worker.WaitIdle(ctx); err != nil {
		log.Warn().Int("running", worker.Running()).Msg("drain deadline reached with jobs running")
	}
	return server.Shutdown(ctx)
}

func maxJobs() int {
	raw := os.Getenv(envMaxJobs)
	if raw == "" {
		return defaultMaxJobs
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		log.Warn().Str("value", raw).Int("default", defaultMaxJobs).
			Msg("CLOUD_WORKER_MAX_JOBS is not a positive number")
		return defaultMaxJobs
	}
	return parsed
}

func launchRunnerProcess(ctx context.Context, jobID uuid.UUID, jobToken string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, self, runCommand)
	cmd.Env = append(
		os.Environ(),
		envJobID+"="+jobID.String(),
		envJobToken+"="+jobToken,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if runErr := cmd.Run(); runErr != nil {
		log.Error().Str("job_id", jobID.String()).Err(runErr).Msg("cloud job process failed")
		return runErr
	}
	log.Info().Str("job_id", jobID.String()).Msg("cloud job process finished")
	return nil
}

func runAssignedJob() error {
	jobID, err := uuid.Parse(os.Getenv(envJobID))
	if err != nil {
		return err
	}
	return cloudworker.Run(context.Background(), cloudworker.Config{
		BaseURL:  os.Getenv(envAPIBaseURL),
		JobID:    jobID,
		JobToken: os.Getenv(envJobToken),
	})
}
