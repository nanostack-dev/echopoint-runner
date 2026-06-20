package engine

import (
	"fmt"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func validateModuleGraph(root flow.Flow, resolver node.ModuleResolver, callStack []string) error {
	if resolver == nil {
		return nil
	}

	parsedFlows := make(map[string]flow.Flow)
	return validateModuleReferences(root, resolver, parsedFlows, callStack)
}

func validateModuleReferences(
	current flow.Flow,
	resolver node.ModuleResolver,
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

		targetFlow, err := resolveModuleFlow(targetFlowID, resolver, parsedFlows)
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
	for _, activeFlowID := range callStack {
		if activeFlowID == targetFlowID {
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
	}
	return nil
}

func resolveModuleFlow(
	flowID string,
	resolver node.ModuleResolver,
	parsedFlows map[string]flow.Flow,
) (flow.Flow, error) {
	if cached, ok := parsedFlows[flowID]; ok {
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

	parsedFlow, err := flow.ParseFromJSONWithOptions(resolvedFlow.FlowDefinition, flow.ParseOptions{
		AllowUnknownInitialInputs: true,
	})
	if err != nil {
		// The referenced flow's own definition fails to parse — the flow author's
		// fault. Classify as a UserError (same code as the execution-time path in
		// moduleExecutor.ExecuteModule) so it logs at debug, not error.
		return flow.Flow{}, spi.NewUserError("MODULE_FLOW_PARSE_FAILED", "parse module flow", err)
	}

	parsedFlows[flowID] = *parsedFlow
	return *parsedFlow, nil
}
