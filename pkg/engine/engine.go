package engine

import (
	"fmt"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/flow"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
)

type Options struct {
	BeforeExecution func(node node.AnyNode)
	AfterExecution  func(node node.AnyNode)
}
type FlowEngine struct {
	flow            flow.Flow
	nodeEdgeOutput  map[node.AnyNode][]node.AnyNode
	nodeEdgeInput   map[node.AnyNode]int
	nodeMap         map[string]node.AnyNode
	beforeExecution func(node node.AnyNode)
	afterExecution  func(node node.AnyNode)
}

func NewFlowEngine(flowInstance flow.Flow, options *Options) (*FlowEngine, error) {
	nodeMap := make(map[string]node.AnyNode, len(flowInstance.Nodes))
	nodeEdgeOutput := make(map[node.AnyNode][]node.AnyNode)
	nodeEdgeInput := make(map[node.AnyNode]int)
	for _, nodeInstance := range flowInstance.Nodes {
		nodeMap[nodeInstance.GetID()] = nodeInstance
		nodeEdgeInput[nodeInstance] = 0
		nodeEdgeOutput[nodeInstance] = nil
	}
	for _, edge := range flowInstance.Edges {
		sourceNode := nodeMap[edge.Source]
		targetNode := nodeMap[edge.Target]
		if sourceNode == nil {
			return nil, fmt.Errorf(
				"source node %s not found in edge to node %s", edge.Source,
				edge.Target,
			)
		}
		if targetNode == nil {
			return nil, fmt.Errorf(
				"target node %s not found in edge to node %s", edge.Target,
				edge.Source,
			)
		}
		nodeEdgeOutput[sourceNode] = append(nodeEdgeOutput[sourceNode], targetNode)
		nodeEdgeInput[targetNode]++
	}
	var beforeExecution func(node node.AnyNode)
	var afterExecution func(node node.AnyNode)
	if options != nil && options.BeforeExecution != nil {
		beforeExecution = options.BeforeExecution
		afterExecution = options.AfterExecution
	}
	return &FlowEngine{
		flowInstance,
		nodeEdgeOutput,
		nodeEdgeInput,
		nodeMap,
		beforeExecution,
		afterExecution,
	}, nil
}

func (engine *FlowEngine) Execute() error {
	var nodeToExecute node.AnyNode
	if len(engine.nodeEdgeInput) == 0 {
		return fmt.Errorf("no nodes to execute")
	}
	for {
		nodeToExecute = engine.foundNodeWithoutInput()
		if nodeToExecute == nil {
			if len(engine.nodeEdgeInput) > 0 {
				return fmt.Errorf(
					"cycle detected or unreachable nodes: %d nodes not executed",
					len(engine.nodeEdgeInput),
				)
			}
			return nil
		}
		if engine.beforeExecution != nil {
			engine.beforeExecution(nodeToExecute)
		}
		pass, err := nodeToExecute.Execute()
		if engine.afterExecution != nil {
			engine.afterExecution(nodeToExecute)
		}
		if err != nil {
			return fmt.Errorf("nodeToExecute failed with %s", err)
		}
		if !pass {
			return nil
		}
		nodeOutput := engine.nodeEdgeOutput[nodeToExecute]
		for _, nodeToReduce := range nodeOutput {
			engine.nodeEdgeInput[nodeToReduce]--
		}
		delete(engine.nodeEdgeInput, nodeToExecute)
	}
}

func (engine *FlowEngine) foundNodeWithoutInput() node.AnyNode {
	for key, value := range engine.nodeEdgeInput {
		if value == 0 {
			return key
		}
	}
	return nil
}
