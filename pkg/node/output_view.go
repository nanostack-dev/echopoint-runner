package node

type outputSnapshot struct {
	outputs map[string]map[string]interface{}
}

func NewOutputView(outputs map[string]map[string]interface{}) OutputView {
	snapshot := make(map[string]map[string]interface{}, len(outputs))
	for nodeID, nodeOutputs := range outputs {
		if nodeOutputs == nil {
			snapshot[nodeID] = nil
			continue
		}

		copied := make(map[string]interface{}, len(nodeOutputs))
		for key, value := range nodeOutputs {
			copied[key] = value
		}
		snapshot[nodeID] = copied
	}

	return outputSnapshot{outputs: snapshot}
}

func (o outputSnapshot) HasNode(nodeID string) bool {
	_, exists := o.outputs[nodeID]
	return exists
}

func (o outputSnapshot) Get(nodeID, outputKey string) (interface{}, bool) {
	nodeOutputs, exists := o.outputs[nodeID]
	if !exists {
		return nil, false
	}

	value, exists := nodeOutputs[outputKey]
	return value, exists
}

func (o outputSnapshot) Node(nodeID string) map[string]interface{} {
	nodeOutputs, exists := o.outputs[nodeID]
	if !exists || nodeOutputs == nil {
		return nil
	}

	copyNodeOutputs := make(map[string]interface{}, len(nodeOutputs))
	for key, value := range nodeOutputs {
		copyNodeOutputs[key] = value
	}

	return copyNodeOutputs
}
