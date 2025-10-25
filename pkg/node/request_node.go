package node

import "time"

type RequestData struct {
	Method      string                 `json:"method"`
	URL         string                 `json:"url"`
	Headers     map[string]string      `json:"headers"`
	QueryParams map[string]interface{} `json:"queryParams"`
	Body        interface{}            `json:"body"`
	Timeout     int                    `json:"timeout"`
	Assertions  []CompositeAssertion   `json:"assertions"`
}

// RequestNode is a typed node for HTTP requests.
type RequestNode struct {
	ID   string      `json:"id"`
	Type Type        `json:"type"`
	Data RequestData `json:"data"`
}

// AsRequestNode safely casts an AnyNode to a RequestNode
// Returns the RequestNode and true if the cast succeeds, nil and false otherwise.
func AsRequestNode(node AnyNode) (*RequestNode, bool) {
	reqNode, ok := node.(*RequestNode)
	return reqNode, ok
}

// MustAsRequestNode casts an AnyNode to a RequestNode, panicking if it fails
// Use this when you're certain the node is a RequestNode.
func MustAsRequestNode(node AnyNode) *RequestNode {
	reqNode, ok := AsRequestNode(node)
	if !ok {
		panic("expected RequestNode but got different type")
	}
	return reqNode
}

func (n *RequestNode) GetID() string {
	return n.ID
}

func (n *RequestNode) GetType() Type {
	return n.Type
}

func (n *RequestNode) Execute() (bool, error) {
	// TODO: Implement
	// delay 1s
	time.Sleep(1 * time.Second)
	return true, nil
}

func (n *RequestNode) GetData() RequestData {
	return n.Data
}
