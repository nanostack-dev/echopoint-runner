package examples

import (
	"fmt"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/edge"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/engine"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/flow"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/operators"
)

// BackendIntegrationExample shows how to use the flow engine from a backend service
func BackendIntegrationExample() {
	// Example 1: Create a flow programmatically
	CreateFlowProgrammatically()

	// Example 2: Load flow from JSON
	LoadFlowFromJSON()

	// Example 3: Validate extractor-operator compatibility
	ValidateConfiguration()
}

// CreateFlowProgrammatically demonstrates creating a flow in Go code
func CreateFlowProgrammatically() {
	// Create nodes
	requestNode := &node.RequestNode{
		ID:   "req-1",
		Type: node.TypeRequest,
		Data: node.RequestData{
			Method:  "GET",
			URL:     "https://api.example.com/users/123",
			Headers: map[string]string{"Accept": "application/json"},
			Timeout: 30000,
			Assertions: []node.CompositeAssertion{
				{
					ExtractorType: string(extractors.ExtractorTypeStatusCode),
					ExtractorData: map[string]interface{}{},
					OperatorType:  string(operators.OperatorTypeBetween),
					OperatorData: map[string]interface{}{
						"min": 200,
						"max": 299,
					},
				},
				{
					ExtractorType: string(extractors.ExtractorTypeJSONPath),
					ExtractorData: map[string]interface{}{
						"path": "$.user.email",
					},
					OperatorType: string(operators.OperatorTypeRegex),
					OperatorData: map[string]interface{}{
						"pattern": `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
					},
				},
			},
		},
	}

	successNode := &node.RequestNode{
		ID:   "success-1",
		Type: node.TypeRequest,
		Data: node.RequestData{
			Method: "POST",
			URL:    "https://api.example.com/notifications",
		},
	}

	// Create flow
	flowInstance := flow.Flow{
		Name:        "User Validation Flow",
		Description: "Validates user data and sends notification",
		Version:     "1.0",
		Nodes:       []node.AnyNode{requestNode, successNode},
		Edges: []edge.Edge{
			{
				ID:     "e1",
				Source: "req-1",
				Target: "success-1",
				Type:   "success",
			},
		},
	}

	// Create engine
	flowEngine, err := engine.NewFlowEngine(
		flowInstance, &engine.Options{
			BeforeExecution: func(n node.AnyNode) {
				fmt.Printf("Executing node: %s\n", n.GetID())
			},
			AfterExecution: func(n node.AnyNode) {
				fmt.Printf("Completed node: %s\n", n.GetID())
			},
		},
	)

	if err != nil {
		fmt.Printf("Failed to create engine: %v\n", err)
		return
	}

	// Execute flow
	if err := flowEngine.Execute(); err != nil {
		fmt.Printf("Flow execution failed: %v\n", err)
		return
	}

	fmt.Println("Flow executed successfully!")
}

// LoadFlowFromJSON demonstrates loading a flow from JSON
func LoadFlowFromJSON() {
	jsonFlow := `{
		"name": "API Health Check",
		"description": "Checks if API is responding correctly",
		"version": "1.0",
		"nodes": [
			{
				"id": "health-check",
				"type": "request",
				"data": {
					"method": "GET",
					"url": "https://api.example.com/health",
					"timeout": 5000,
					"assertions": []
				}
			}
		],
		"edges": []
	}`

	// Parse flow from JSON
	flowInstance, err := flow.ParseFromJSON([]byte(jsonFlow))
	if err != nil {
		fmt.Printf("Failed to parse flow: %v\n", err)
		return
	}

	fmt.Printf("Loaded flow: %s\n", flowInstance.Name)
}

// ValidateConfiguration demonstrates runtime validation
func ValidateConfiguration() {
	// Valid combination
	if extractors.IsOperatorCompatible(
		extractors.ExtractorTypeStatusCode,
		operators.OperatorTypeBetween,
	) {
		fmt.Println("✓ StatusCode + Between is valid")
	}

	// Invalid combination
	if !extractors.IsOperatorCompatible(
		extractors.ExtractorTypeStatusCode,
		operators.OperatorTypeContains,
	) {
		fmt.Println("✗ StatusCode + Contains is invalid (as expected)")
	}

	// Get compatible operators
	compatibleOps := extractors.GetCompatibleOperators(extractors.ExtractorTypeHeader)
	fmt.Printf("Header extractor supports %d operators\n", len(compatibleOps))
}

// BuildAssertion is a helper to build CompositeAssertion from factories
func BuildAssertion() {
	// Using operator factories for type safety
	str := operators.StringOperators{}
	num := operators.NumberOperators{}

	// String assertion
	stringOp := str.Contains("success")
	fmt.Printf("String operator type: %s\n", stringOp.GetType())

	// Number assertion
	numberOp := num.Between(200, 299)
	fmt.Printf("Number operator type: %s\n", numberOp.GetType())

	// These can be used in assertions:
	assertion := node.CompositeAssertion{
		ExtractorType: string(extractors.ExtractorTypeJSONPath),
		ExtractorData: map[string]interface{}{
			"path": "$.status",
		},
		OperatorType: string(stringOp.GetType()),
		OperatorData: map[string]interface{}{
			"substring": "success",
		},
	}

	fmt.Printf("Created assertion: %+v\n", assertion)
}
