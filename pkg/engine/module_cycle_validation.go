package engine

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func validateModuleGraph(root flow.Flow, resolver spi.ModuleResolver, callStack []string) error {
	if resolver == nil {
		return nil
	}

	parsedFlows := make(map[string]flow.Flow)
	return validateModuleReferences(root, resolver, parsedFlows, callStack)
}

func validateModuleReferences(
	current flow.Flow,
	resolver spi.ModuleResolver,
	parsedFlows map[string]flow.Flow,
	callStack []string,
) error {
	for _, currentNode := range current.Nodes {
		moduleNode, ok := node.AsModuleNode(currentNode)
		if !ok {
			continue
		}

		targetFlowID := strings.TrimSpace(moduleNode.Data.FlowID)
		if targetFlowID == "" {
			continue
		}

		if cycleErr := detectModuleCycle(callStack, targetFlowID); cycleErr != nil {
			return cycleErr
		}

		targetFlow, err := resolveModuleFlow(
			targetFlowID, resolver, parsedFlows, slices.Collect(maps.Keys(moduleNode.Data.InputBindings)),
		)
		if err != nil {
			return err
		}

		nextStack := append(cloneStringSlice(callStack), targetFlowID)
		validateErr := validateModuleReferences(targetFlow, resolver, parsedFlows, nextStack)
		if validateErr != nil {
			return validateErr
		}
	}

	return nil
}

func detectModuleCycle(callStack []string, targetFlowID string) error {
	if slices.Contains(callStack, targetFlowID) {
		cycle := append(cloneStringSlice(callStack), targetFlowID)
		// A module cycle is an invalid flow graph authored by the user, not a
		// runner fault. Classify it as a UserError so the node executor logs it
		// at debug instead of error — mirroring moduleExecutor.ExecuteModule and
		// keeping invalid flow definitions out of error-rate alerts.
		return spi.NewUserError(
			"MODULE_CYCLE_DETECTED",
			fmt.Sprintf("module cycle detected: %s", strings.Join(cycle, " -> ")),
			nil,
		)
	}
	return nil
}

// resolveModuleFlow parses a module's target flow knowing the inputs it will
// receive (its bindings and the flow's overrides), so a path into one, such as
// {{account.id}}, validates as it will at execution time.
func resolveModuleFlow(
	flowID string,
	resolver spi.ModuleResolver,
	parsedFlows map[string]flow.Flow,
	boundInputs []string,
) (flow.Flow, error) {
	slices.Sort(boundInputs)
	cacheKey := flowID + "\x00" + strings.Join(boundInputs, ",")
	if cached, ok := parsedFlows[cacheKey]; ok {
		return cached, nil
	}

	resolvedFlow, ok := resolver.ResolveFlow(flowID)
	if !ok {
		// A dangling module reference is a flow-definition fault, not a runner
		// fault — classify as a UserError so it logs at debug, not error.
		return flow.Flow{}, spi.NewUserError(
			"MODULE_FLOW_NOT_FOUND",
			fmt.Sprintf("referenced flow %q not found", flowID),
			nil,
		)
	}

	inputKeys := append(slices.Clone(boundInputs), slices.Collect(maps.Keys(resolvedFlow.InputOverrides))...)
	parsedFlow, err := flow.ParseFromJSONWithOptions(resolvedFlow.FlowDefinition, flow.ParseOptions{
		AllowedInitialInputKeys:   inputKeys,
		AllowUnknownInitialInputs: true,
	})
	if err != nil {
		// The referenced flow's own definition fails to parse — the flow author's
		// fault. Classify as a UserError (same code as the execution-time path in
		// moduleExecutor.ExecuteModule) so it logs at debug, not error.
		return flow.Flow{}, spi.NewUserError("MODULE_FLOW_PARSE_FAILED", "parse module flow", err)
	}

	parsedFlows[cacheKey] = *parsedFlow
	return *parsedFlow, nil
}
