package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/nanostack-dev/echopoint-runner/internal/config"
	internalLogger "github.com/nanostack-dev/echopoint-runner/internal/logger"
	runnerruntime "github.com/nanostack-dev/echopoint-runner/internal/runtime"
	"github.com/rs/zerolog/log"
)

func main() {
	if err := run(); err != nil {
		log.Error().Err(err).Msg("runner exited with error")
		os.Exit(1)
	}
}

func run() error {
	configValue, err := config.Load()
	if err != nil {
		return err
	}

	internalLogger.InitLogger(configValue.LogLevel, internalLogger.Format(configValue.LogFormat))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtime := runnerruntime.New(configValue)
	log.Info().
		Str("runner_id", configValue.RunnerID).
		Str("boot_id", runtime.BootID().String()).
		Str("base_url", configValue.BaseURL).
		Str("organization_id", configValue.OrganizationID).
		Msg("runner booted")

	runErr := runtime.Run(ctx)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}

	log.Info().Msg("runner stopped")
	return nil
}
