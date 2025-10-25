package examples

import (
	"fmt"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
)

// Example showing how to use node casting utilities
func ExampleNodeCasting(anyNode node.AnyNode) {
	// Safe casting with AsRequestNode
	if reqNode, ok := node.AsRequestNode(anyNode); ok {
		data := reqNode.GetData()
		fmt.Printf("Request: %s %s\n", data.Method, data.URL)
		fmt.Printf("Assertions: %d\n", len(data.Assertions))
	}

	// Alternative: using MustAsRequestNode when you're certain of the type
	// This will panic if the node is not a RequestNode
	reqNode := node.MustAsRequestNode(anyNode)
	data := reqNode.GetData()
	fmt.Printf("Method: %s\n", data.Method)

	// Working with assertions
	for i, assertion := range data.Assertions {
		fmt.Printf(
			"Assertion %d: %s %s %v\n", i, assertion.Type, assertion.Operator, assertion.Expected,
		)
	}
}
