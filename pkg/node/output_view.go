package node

import "maps"

type outputSnapshot struct {
	outputs map[string]map[string]any
}

func NewOutputView(outputs map[string]map[string]any) OutputView {
	snapshot := make(map[string]map[string]any, len(outputs))
	for nodeID, nodeOutputs := range outputs {
		if nodeOutputs == nil {
			snapshot[nodeID] = nil
			continue
		}

		copied := make(map[string]any, len(nodeOutputs))
		maps.Copy(copied, nodeOutputs)
		snapshot[nodeID] = copied
	}

	return outputSnapshot{outputs: snapshot}
}

func (o outputSnapshot) HasNode(nodeID string) bool {
	_, exists := o.outputs[nodeID]
	return exists
}

func (o outputSnapshot) Get(nodeID, outputKey string) (any, bool) {
	nodeOutputs, exists := o.outputs[nodeID]
	if !exists {
		return nil, false
	}

	value, exists := nodeOutputs[outputKey]
	return value, exists
}

func (o outputSnapshot) Node(nodeID string) map[string]any {
	nodeOutputs, exists := o.outputs[nodeID]
	if !exists || nodeOutputs == nil {
		return nil
	}

	copyNodeOutputs := make(map[string]any, len(nodeOutputs))
	maps.Copy(copyNodeOutputs, nodeOutputs)

	return copyNodeOutputs
}
