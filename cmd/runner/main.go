package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/nanostack-dev/echopoint-runner/internal/config"
	"github.com/nanostack-dev/echopoint-runner/internal/ephemeral"
	internalLogger "github.com/nanostack-dev/echopoint-runner/internal/logger"
	runnerruntime "github.com/nanostack-dev/echopoint-runner/internal/runtime"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}

// errServeFailed marks a long-lived (serve) runner failure. It exits 1 to preserve the
// pre-cobra long-lived runner behaviour; the underlying error is logged before returning.
var errServeFailed = errors.New("serve failed")

// exitCode maps known sentinel errors to stable exit codes:
//
//   - ErrFlowFailed  → 1  (ephemeral flow ran but produced a failed result)
//   - errServeFailed → 1  (long-lived runner exited with an error; preserves prior behaviour)
//   - anything else  → 3  (API/runner/contract/infra error)
func exitCode(err error) int {
	if errors.Is(err, ephemeral.ErrFlowFailed) || errors.Is(err, errServeFailed) {
		return 1
	}
	return 3 //nolint:mnd // 3 = API/runner/contract error per CLI contract
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "echopoint-runner",
		Short:         "Echopoint flow execution runtime",
		SilenceErrors: true,
		SilenceUsage:  true,
		// Backward compatibility: existing deployments (Dockerfile ENTRYPOINT, infra)
		// invoke the binary with no subcommand and expect the long-lived self-hosted
		// runner. Default the root command to that behaviour so "echopoint-runner" keeps
		// working exactly as before, while still allowing explicit "serve"/"ephemeral".
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLongLivedRunner()
		},
	}

	root.AddCommand(newServeCommand())
	root.AddCommand(ephemeral.NewCommand())

	return root
}

// newServeCommand wraps the existing long-lived runner logic as a cobra command so that
// "echopoint-runner serve" is an explicit alias for the default no-subcommand behaviour.
func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the long-lived self-hosted runner (claims queued jobs)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLongLivedRunner()
		},
	}
}

func runLongLivedRunner() error {
	configValue, err := config.Load()
	if err != nil {
		// Logger not yet initialised from config; the default logger writes to stderr.
		log.Error().Err(err).Msg("failed to load runner config")
		return errServeFailed
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
		// Log the underlying error and exit 1, matching the pre-cobra long-lived behaviour.
		log.Error().Err(runErr).Msg("runner exited with error")
		return errServeFailed
	}

	log.Info().Msg("runner stopped")
	return nil
}
