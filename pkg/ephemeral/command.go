package ephemeral

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	internalLogger "github.com/nanostack-dev/echopoint-runner/internal/logger"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const stdinMarker = "-"

// ErrFlowFailed is returned when the flow itself fails (exit 1 in the binary).
var ErrFlowFailed = errors.New("flow execution failed")

// NewCommand builds and returns the cobra "ephemeral" subcommand.
// Logs are directed to stderr; the result JSON is the only stdout output.
func NewCommand() *cobra.Command {
	var inputPath string
	var outputPath string

	cmd := &cobra.Command{
		Use:   "ephemeral",
		Short: "Execute a flow from an ephemeral execution package",
		Long: `Read a flow execution package from --input (path or '-' for stdin),
execute the flow locally using the embedded engine, and write the result JSON
to --output (path or '-' for stdout).

All diagnostic logs go to stderr. Only the result JSON goes to stdout when
--output is '-'.

Does not call runner/jobs/next, heartbeat, progress, or complete.
Does not require ECHOPOINT_RUNNER_API_KEY.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			internalLogger.InitLogger(zerolog.InfoLevel, internalLogger.JSON)

			pkg, err := readPackage(inputPath, cmd.InOrStdin())
			if err != nil {
				log.Error().Err(err).Str("input", inputPath).Msg("failed to read ephemeral package")
				// Emit a structured failed result when the package parsed far enough to
				// identify the execution, so the CLI/server can record the failure.
				if pkg != nil && pkg.ExecutionID != "" {
					code := "INVALID_PACKAGE"
					result := failedResult(time.Now().UTC(), err.Error(), &code)
					if writeErr := writeResult(outputPath, result, cmd.OutOrStdout()); writeErr != nil {
						log.Error().Err(writeErr).Str("output", outputPath).
							Msg("failed to write invalid-package result")
					}
				}
				return fmt.Errorf("read package: %w", err)
			}

			result := Run(pkg)

			if writeErr := writeResult(outputPath, result, cmd.OutOrStdout()); writeErr != nil {
				log.Error().Err(writeErr).Str("output", outputPath).Msg("failed to write ephemeral result")
				return fmt.Errorf("write result: %w", writeErr)
			}

			if result.Status == "failed" {
				return ErrFlowFailed
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&inputPath, "input", stdinMarker, "path to ephemeral package JSON, or '-' to read from stdin")
	cmd.Flags().StringVar(&outputPath, "output", stdinMarker, "path to write result JSON, or '-' to write to stdout")

	return cmd
}

func readPackage(path string, stdinReader io.Reader) (*Package, error) {
	var reader io.Reader
	if path == stdinMarker {
		reader = stdinReader
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open input file %q: %w", path, err)
		}
		defer func() {
			if closeErr := f.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Str("path", path).Msg("failed to close input file")
			}
		}()
		reader = f
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}

	var pkg Package
	if err = json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse package JSON: %w", err)
	}

	// Return the partially-parsed package alongside validation errors so the caller can
	// emit a structured failed result when an execution_id is present.
	if pkg.ExecutionID == "" {
		return &pkg, errors.New("package missing execution_id")
	}
	if pkg.FlowID == "" {
		return &pkg, errors.New("package missing flow_id")
	}
	if len(pkg.FlowDefinition) == 0 {
		return &pkg, errors.New("package missing flow_definition")
	}

	return &pkg, nil
}

func writeResult(path string, result Result, stdoutFallback io.Writer) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	if path == stdinMarker {
		_, err = fmt.Fprintln(stdoutFallback, string(encoded))
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output file %q: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("path", path).Msg("failed to close output file")
		}
	}()

	if _, writeErr := fmt.Fprintln(f, string(encoded)); writeErr != nil {
		return fmt.Errorf("write result to file: %w", writeErr)
	}
	return nil
}
