package engine

import (
	"fmt"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
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
			return fmt.Errorf("module cycle detected: %s", strings.Join(cycle, " -> "))
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
		return flow.Flow{}, fmt.Errorf("referenced flow %q not found", flowID)
	}

	parsedFlow, err := flow.ParseFromJSONWithOptions(resolvedFlow.FlowDefinition, flow.ParseOptions{
		AllowUnknownInitialInputs: true,
	})
	if err != nil {
		return flow.Flow{}, fmt.Errorf("parse module flow: %w", err)
	}

	parsedFlows[flowID] = *parsedFlow
	return *parsedFlow, nil
}
