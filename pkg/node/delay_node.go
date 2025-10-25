package node

type DelayData struct {
	Duration int `json:"duration"` // Duration in milliseconds
}

// DelayNode is a typed node for delays
type DelayNode struct {
	ID   string    `json:"id"`
	Type Type      `json:"type"`
	Data DelayData `json:"data"`
}

func (n *DelayNode) GetID() string {
	return n.ID
}

func (n *DelayNode) GetType() Type {
	return n.Type
}

func (n *DelayNode) Execute() (bool, error) {
	// TODO: Implement
	return true, nil
}

func (n *DelayNode) GetData() DelayData {
	return n.Data
}

// AsDelayNode safely casts an AnyNode to a DelayNode
// Returns the DelayNode and true if the cast succeeds, nil and false otherwise
func AsDelayNode(node AnyNode) (*DelayNode, bool) {
	delayNode, ok := node.(*DelayNode)
	return delayNode, ok
}

// MustAsDelayNode casts an AnyNode to a DelayNode, panicking if it fails
// Use this when you're certain the node is a DelayNode
func MustAsDelayNode(node AnyNode) *DelayNode {
	delayNode, ok := AsDelayNode(node)
	if !ok {
		panic("expected DelayNode but got different type")
	}
	return delayNode
}
