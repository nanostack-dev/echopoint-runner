package flow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
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

	if err := validateContractNodes(parsedFlow.Nodes, options); err != nil {
		return err
	}

	outputsByNode := make(map[string]map[string]struct{}, len(parsedFlow.Nodes))
	openOutputNodes := make(map[string]struct{})
	for _, currentNode := range parsedFlow.Nodes {
		nodeID := strings.TrimSpace(currentNode.GetID())
		if nodeID != "" {
			outputsByNode[nodeID] = buildOutputSet(currentNode.OutputSchema())
			if mod, isMod := node.AsModuleNode(currentNode); isMod && len(mod.Data.OutputBindings) == 0 {
				openOutputNodes[nodeID] = struct{}{}
			}
		}
	}

	predecessorsByNode := buildPredecessors(parsedFlow)
	for _, currentNode := range parsedFlow.Nodes {
		if err := validateNodeReferences(
			currentNode,
			availableInitialInputs,
			upstreamOutputs(strings.TrimSpace(currentNode.GetID()), predecessorsByNode, outputsByNode),
			openOutputNodes,
			options,
		); err != nil {
			return err
		}
	}

	if err := validateBranchTargets(parsedFlow); err != nil {
		return err
	}

	return nil
}

func validateContractNodes(nodes []node.AnyNode, options ParseOptions) error {
	var inputCount, outputCount int
	for _, currentNode := range nodes {
		if err := validateContractNode(currentNode, options, &inputCount, &outputCount); err != nil {
			return err
		}
	}
	return nil
}

func validateContractNode(
	currentNode node.AnyNode,
	options ParseOptions,
	inputCount *int,
	outputCount *int,
) error {
	switch currentNode.GetType() { //nolint:exhaustive // Only contract and inline-body nodes are gated here.
	case spi.KindInput:
		return validateSingleContractNode(
			currentNode.GetID(), "input", options.AllowInputNodes, "AllowInputNodes", inputCount,
		)
	case spi.KindOutput:
		return validateSingleContractNode(
			currentNode.GetID(), "output", options.AllowOutputNodes, "AllowOutputNodes", outputCount,
		)
	case spi.KindLoop:
		if loopNode, ok := node.AsLoopNode(currentNode); ok {
			return rejectInlineBodyContractNodes(currentNode.GetID(), loopNode.Data.Body)
		}
	case spi.KindPoll:
		if pollNode, ok := node.AsPollNode(currentNode); ok {
			return rejectInlineBodyContractNodes(currentNode.GetID(), pollNode.Data.Body)
		}
	}
	return nil
}

func validateSingleContractNode(nodeID, kind string, allowed bool, flag string, count *int) error {
	if !allowed {
		return fmt.Errorf("node %s: %s node is not allowed when %s is false", nodeID, kind, flag)
	}
	*count++
	if *count > 1 {
		return fmt.Errorf("node %s: a flow may declare at most one %s node", nodeID, kind)
	}
	return nil
}

// rejectInlineBodyContractNodes rejects input and output nodes inside inline
// loop or poll bodies at parse time, so a test flow cannot smuggle contract
// nodes through a body sub-flow.
func rejectInlineBodyContractNodes(parentID string, rawBody json.RawMessage) error {
	if len(rawBody) == 0 {
		return nil
	}
	var body struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return nil //nolint:nilerr // Malformed bodies are surfaced by the body's own parse path.
	}
	for _, rawNode := range body.Nodes {
		var peek struct {
			ID   string   `json:"id"`
			Type spi.Kind `json:"type"`
			Data struct {
				Body json.RawMessage `json:"body"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rawNode, &peek); err != nil {
			continue
		}
		if peek.Type == spi.KindInput || peek.Type == spi.KindOutput {
			return fmt.Errorf(
				"node %s: %s node %q is not allowed inside a loop or poll body",
				parentID,
				peek.Type,
				peek.ID,
			)
		}
		if peek.Type == spi.KindLoop || peek.Type == spi.KindPoll {
			if err := rejectInlineBodyContractNodes(peek.ID, peek.Data.Body); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildPredecessors(parsedFlow *Flow) map[string][]string {
	predecessorsByNode := make(map[string][]string, len(parsedFlow.Nodes))
	for _, currentEdge := range parsedFlow.Edges {
		source := strings.TrimSpace(currentEdge.Source)
		target := strings.TrimSpace(currentEdge.Target)
		if source == "" || target == "" {
			continue
		}
		predecessorsByNode[target] = append(predecessorsByNode[target], source)
	}
	return predecessorsByNode
}

// upstreamOutputs returns the outputs of every node with an edge path to
// nodeID. Only those nodes are guaranteed to have finished when nodeID starts,
// whatever order the definition lists its nodes in.
func upstreamOutputs(
	nodeID string,
	predecessorsByNode map[string][]string,
	outputsByNode map[string]map[string]struct{},
) map[string]map[string]struct{} {
	upstream := make(map[string]map[string]struct{})
	visited := map[string]struct{}{nodeID: {}}
	pending := slices.Clone(predecessorsByNode[nodeID])
	for len(pending) > 0 {
		ancestorID := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, seen := visited[ancestorID]; seen {
			continue
		}
		visited[ancestorID] = struct{}{}
		if outputs, known := outputsByNode[ancestorID]; known {
			upstream[ancestorID] = outputs
		}
		pending = append(pending, predecessorsByNode[ancestorID]...)
	}
	return upstream
}

// validateBranchTargets ensures every branch case.Target and Default names a
// real successor of the branch (an edge branch->target exists). A target that is
// not a successor edge would silently dead-edge every real successor at runtime,
// skipping the whole downstream subtree while the flow still reports Success.
// Catching it at parse time turns that silent misroute into a clear error.
func validateBranchTargets(parsedFlow *Flow) error {
	successorsBySource := make(map[string]map[string]struct{}, len(parsedFlow.Edges))
	for _, currentEdge := range parsedFlow.Edges {
		source := strings.TrimSpace(currentEdge.Source)
		target := strings.TrimSpace(currentEdge.Target)
		if source == "" || target == "" {
			continue
		}
		if successorsBySource[source] == nil {
			successorsBySource[source] = make(map[string]struct{})
		}
		successorsBySource[source][target] = struct{}{}
	}

	for _, currentNode := range parsedFlow.Nodes {
		branchNode, ok := node.AsBranchNode(currentNode)
		if !ok {
			continue
		}
		branchID := branchNode.GetID()
		successors := successorsBySource[branchID]

		data := branchNode.GetData()
		for _, branchCase := range data.Cases {
			if err := validateBranchTarget(branchID, branchCase.Target, successors); err != nil {
				return err
			}
		}
		if err := validateBranchTarget(branchID, data.Default, successors); err != nil {
			return err
		}
	}

	return nil
}

func validateBranchTarget(
	branchID string,
	target string,
	successors map[string]struct{},
) error {
	trimmedTarget := strings.TrimSpace(target)
	if trimmedTarget == "" {
		return nil
	}
	if _, ok := successors[trimmedTarget]; ok {
		return nil
	}
	return fmt.Errorf(
		"branch node %s: routing target '%s' is not a successor node "+
			"(no edge %s->%s exists)",
		branchID,
		trimmedTarget,
		branchID,
		trimmedTarget,
	)
}

func validateNodeReferences(
	currentNode node.AnyNode,
	availableInitialInputs map[string]struct{},
	availableNodeOutputs map[string]map[string]struct{},
	openOutputNodes map[string]struct{},
	options ParseOptions,
) error {
	nodeID := currentNode.GetID()
	refs := append(slices.Clone(currentNode.InputSchema()), node.AssertionReferences(currentNode)...)
	if selfResolving, ok := currentNode.(node.SelfResolvingNode); ok {
		refs = append(refs, selfResolving.ResolvedReferences()...)
	}
	for _, ref := range refs {
		if _, isFlowInput := availableInitialInputs[strings.TrimSpace(ref)]; isFlowInput {
			continue
		}
		sourceNodeID, outputKey, err := parseReference(ref)
		if err != nil {
			return fmt.Errorf("node %s: invalid input reference '%s': %w", nodeID, ref, err)
		}
		inputRooted := hasPathRoot(strings.TrimSpace(ref), availableInitialInputs)
		if _, isNode := availableNodeOutputs[sourceNodeID]; !isNode && inputRooted {
			continue
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
			openOutputNodes,
		); validateErr != nil {
			// A step of the same name that never publishes the path leaves it to
			// the flow input at run time (resolveDataRef), so accept it here too.
			if inputRooted {
				continue
			}
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
	openOutputNodes map[string]struct{},
) error {
	outputs, ok := availableNodeOutputs[sourceNodeID]
	if !ok {
		if currentNode.GetRunWhen() == spi.RunWhenAlways {
			return nil
		}
		return fmt.Errorf(
			"node %s: source node '%s' not available for input '%s'",
			currentNode.GetID(),
			sourceNodeID,
			ref,
		)
	}
	if _, open := openOutputNodes[sourceNodeID]; open {
		return nil
	}
	if hasPathRoot(outputKey, outputs) || currentNode.GetRunWhen() == spi.RunWhenAlways {
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

// hasPathRoot reports whether ref, or a dotted prefix of it that a path such as
// item.id or accounts.0.id walks into at run time, is one of keys.
func hasPathRoot(ref string, keys map[string]struct{}) bool {
	for candidate := ref; ; {
		if _, ok := keys[candidate]; ok {
			return true
		}
		end := strings.LastIndex(candidate, ".")
		if end <= 0 {
			return false
		}
		candidate = candidate[:end]
	}
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
