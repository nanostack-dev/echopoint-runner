package ephemeral

import (
	"fmt"
	"sort"
	"time"

	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/pkg/dynamicvars"
	flowpkg "github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/rs/zerolog/log"
)

// Run executes the flow described by pkg and returns the structured result.
// Inputs are treated as secrets: only key names are logged, never values.
// No control-plane clients (claim/heartbeat/complete) are constructed or called.
func Run(pkg *Package) Result {
	startedAt := time.Now().UTC()

	inputKeys := sortedKeys(pkg.Inputs)
	log.Info().
		Str("execution_id", pkg.ExecutionID).
		Str("flow_id", pkg.FlowID).
		Strs("input_keys", inputKeys).
		Msg("starting ephemeral execution")

	flowDef, err := flowpkg.ParseFromJSONWithOptions(pkg.FlowDefinition, flowpkg.ParseOptions{
		AllowedInitialInputKeys: inputKeys,
	})
	if err != nil {
		log.Error().
			Str("execution_id", pkg.ExecutionID).
			Str("flow_id", pkg.FlowID).
			Err(err).
			Msg("failed to parse flow definition")
		return failedResult(startedAt, fmt.Sprintf("parse flow definition: %v", err), nil)
	}

	execResult, execErr := runner.Run(*flowDef, pkg.Inputs,
		runner.WithReferencedFlows(pkg.ReferencedFlows),
		runner.WithDynamicVars(dynamicvars.New(pkg.ExecutionID)),
	)

	completedAt := time.Now().UTC()
	durationMs := completedAt.Sub(startedAt).Milliseconds()

	if execErr != nil {
		errorMsg := execErr.Error()
		var errorCode *string
		if execResult != nil {
			errorCode = execResult.ErrorCode
			if execResult.ErrorMsg != nil && *execResult.ErrorMsg != "" {
				errorMsg = *execResult.ErrorMsg
			}
		}
		log.Error().
			Str("execution_id", pkg.ExecutionID).
			Str("flow_id", pkg.FlowID).
			Str("error_message", errorMsg).
			Int64("duration_ms", durationMs).
			Msg("ephemeral execution failed")

		payload, payloadErr := toPayload(execResult)
		if payloadErr != nil {
			log.Error().
				Err(payloadErr).
				Str("execution_id", pkg.ExecutionID).
				Msg("failed to encode partial execution result for failed flow")
		}

		result := failedResult(startedAt, errorMsg, errorCode)
		result.CompletedAt = completedAt
		result.DurationMs = durationMs
		result.Result = payload
		return result
	}

	payload, payloadErr := toPayload(execResult)
	if payloadErr != nil {
		return failedResult(startedAt, fmt.Sprintf("encode execution result: %v", payloadErr), nil)
	}

	log.Info().
		Str("execution_id", pkg.ExecutionID).
		Str("flow_id", pkg.FlowID).
		Int64("duration_ms", durationMs).
		Msg("ephemeral execution completed")

	return Result{
		Status:      "completed",
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		DurationMs:  durationMs,
		Result:      payload,
	}
}

func failedResult(startedAt time.Time, errorMsg string, errorCode *string) Result {
	now := time.Now().UTC()
	return Result{
		Status:       "failed",
		StartedAt:    startedAt,
		CompletedAt:  now,
		DurationMs:   now.Sub(startedAt).Milliseconds(),
		Result:       nil,
		ErrorMessage: &errorMsg,
		ErrorCode:    errorCode,
	}
}

func toPayload(result *node.FlowExecutionResult) (*map[string]any, error) {
	payload, err := controlplane.FlowExecutionResultToPayload(result)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
