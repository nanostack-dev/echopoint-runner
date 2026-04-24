package flow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

const referencePartCount = 2

func validateFlowReferences(parsedFlow *Flow, options ParseOptions) error {
	if parsedFlow == nil {
		return nil
	}

	availableInitialInputs := make(
		map[string]struct{},
		len(parsedFlow.InitialInputs)+len(options.AllowedInitialInputKeys),
	)
	for key := range parsedFlow.InitialInputs {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey != "" {
			availableInitialInputs[trimmedKey] = struct{}{}
		}
	}
	for _, key := range options.AllowedInitialInputKeys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey != "" {
			availableInitialInputs[trimmedKey] = struct{}{}
		}
	}

	availableNodeOutputs := make(map[string]map[string]struct{}, len(parsedFlow.Nodes))
	for _, currentNode := range parsedFlow.Nodes {
		if err := validateNodeReferences(
			currentNode,
			availableInitialInputs,
			availableNodeOutputs,
			options,
		); err != nil {
			return err
		}

		nodeID := strings.TrimSpace(currentNode.GetID())
		if nodeID == "" {
			continue
		}
		availableNodeOutputs[nodeID] = buildOutputSet(currentNode.OutputSchema())
	}

	return nil
}

func validateNodeReferences(
	currentNode node.AnyNode,
	availableInitialInputs map[string]struct{},
	availableNodeOutputs map[string]map[string]struct{},
	options ParseOptions,
) error {
	nodeID := currentNode.GetID()
	for _, ref := range currentNode.InputSchema() {
		sourceNodeID, outputKey, err := parseReference(ref)
		if err != nil {
			return fmt.Errorf("node %s: invalid input reference '%s': %w", nodeID, ref, err)
		}

		if sourceNodeID == "" {
			if validateErr := validateInitialInputReference(
				nodeID,
				ref,
				outputKey,
				availableInitialInputs,
				options,
			); validateErr != nil {
				return validateErr
			}
			continue
		}

		if validateErr := validateNodeOutputReference(
			currentNode,
			ref,
			sourceNodeID,
			outputKey,
			availableNodeOutputs,
		); validateErr != nil {
			return validateErr
		}
	}

	return nil
}

func validateInitialInputReference(
	nodeID string,
	ref string,
	outputKey string,
	availableInitialInputs map[string]struct{},
	options ParseOptions,
) error {
	if _, ok := availableInitialInputs[outputKey]; ok || options.AllowUnknownInitialInputs {
		return nil
	}

	return fmt.Errorf(
		"node %s: input '%s' references unknown initial variable '%s'",
		nodeID,
		ref,
		outputKey,
	)
}

func validateNodeOutputReference(
	currentNode node.AnyNode,
	ref string,
	sourceNodeID string,
	outputKey string,
	availableNodeOutputs map[string]map[string]struct{},
) error {
	outputs, ok := availableNodeOutputs[sourceNodeID]
	if !ok {
		if currentNode.GetRunWhen() == node.RunWhenAlways {
			return nil
		}
		return fmt.Errorf(
			"node %s: source node '%s' not available for input '%s'",
			currentNode.GetID(),
			sourceNodeID,
			ref,
		)
	}
	if _, outputExists := outputs[outputKey]; outputExists || currentNode.GetRunWhen() == node.RunWhenAlways {
		return nil
	}

	return fmt.Errorf(
		"node %s: output '%s' not declared by source node '%s'",
		currentNode.GetID(),
		outputKey,
		sourceNodeID,
	)
}

func buildOutputSet(outputSchema []string) map[string]struct{} {
	outputs := make(map[string]struct{}, len(outputSchema))
	for _, outputName := range outputSchema {
		trimmedName := strings.TrimSpace(outputName)
		if trimmedName != "" {
			outputs[trimmedName] = struct{}{}
		}
	}
	return outputs
}

func parseReference(ref string) (string, string, error) {
	trimmedRef := strings.TrimSpace(ref)
	if trimmedRef == "" {
		return "", "", errors.New("reference is empty")
	}

	parts := strings.SplitN(trimmedRef, ".", referencePartCount)
	if len(parts) == referencePartCount {
		sourceNodeID := strings.TrimSpace(parts[0])
		outputKey := strings.TrimSpace(parts[1])
		if sourceNodeID == "" || outputKey == "" {
			return "", "", errors.New("reference must use the form nodeId.outputKey or variableName")
		}
		return sourceNodeID, outputKey, nil
	}

	return "", trimmedRef, nil
}
