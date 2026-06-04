package ephemeral

import (
	"fmt"
	"sort"
	"time"

	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/pkg/dynamicvars"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	flowpkg "github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
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

	inputs := mergeInputs(flowDef.InitialInputs, pkg.Inputs)

	execResult, execErr := engine.ExecuteFlowDefinition(*flowDef, inputs, &engine.ExecuteOptions{
		Observer:       engine.NoopObserver{},
		ModuleResolver: buildModuleResolver(pkg),
		DynamicVars:    dynamicvars.New(pkg.ExecutionID),
	})

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

func toPayload(result *node.FlowExecutionResult) (*map[string]interface{}, error) {
	payload, err := controlplane.FlowExecutionResultToPayload(result)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

type referencedFlowResolver struct {
	flows map[string]node.ResolvedModuleFlow
}

func (r referencedFlowResolver) ResolveFlow(flowID string) (node.ResolvedModuleFlow, bool) {
	resolved, ok := r.flows[flowID]
	return resolved, ok
}

func buildModuleResolver(pkg *Package) node.ModuleResolver {
	if len(pkg.ReferencedFlows) == 0 {
		return nil
	}
	resolved := make(map[string]node.ResolvedModuleFlow, len(pkg.ReferencedFlows))
	for flowID, ref := range pkg.ReferencedFlows {
		resolved[flowID] = node.ResolvedModuleFlow{
			FlowDefinition: ref.FlowDefinition,
			InputOverrides: cloneInputs(ref.InputOverrides),
		}
	}
	return referencedFlowResolver{flows: resolved}
}

func cloneInputs(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}

func mergeInputs(base, override map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
