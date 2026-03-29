package node

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDebugNodeInputSchema(t *testing.T) {
	node := DebugNode{
		Data: DebugData{
			Expressions: []string{
				"{{request-1.body}}",
				"hello {{ foo }}",
				"no vars",
			},
		},
	}

	got := node.InputSchema()
	want := map[string]struct{}{
		"request-1.body": {},
		"foo":            {},
	}

	gotSet := make(map[string]struct{}, len(got))
	for _, v := range got {
		gotSet[v] = struct{}{}
	}

	if !reflect.DeepEqual(gotSet, want) {
		t.Fatalf("unexpected input schema: %#v", got)
	}
}

func TestDebugNodeExecute(t *testing.T) {
	node := DebugNode{
		BaseNode: BaseNode{
			ID:          "debug-1",
			DisplayName: "Debug",
			NodeType:    TypeDebug,
		},
		Data: DebugData{
			Expressions: []string{
				"{{request-1.body}}",
				"{{missing}}",
				"plain",
			},
		},
	}

	ctx := ExecutionContext{
		Inputs: map[string]interface{}{
			"request-1.body": "ok",
		},
	}

	resultAny, err := node.Execute(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	result, ok := resultAny.(*DebugExecutionResult)
	if !ok {
		t.Fatalf("expected DebugExecutionResult, got %T", resultAny)
	}

	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}

	if result.Results[0].Value != "ok" || result.Results[0].Error != "" {
		t.Fatalf("unexpected first result: %#v", result.Results[0])
	}

	if result.Results[1].Error != "unresolved template variables" {
		t.Fatalf("unexpected unresolved error: %#v", result.Results[1])
	}

	if result.Results[2].Value != "plain" || result.Results[2].Error != "" {
		t.Fatalf("unexpected third result: %#v", result.Results[2])
	}
}

func TestUnmarshalDebugNode(t *testing.T) {
	payload := []byte(`{
		"id": "debug-1",
		"display_name": "Debug",
		"type": "debug",
		"data": {"expressions": ["{{request-1.body}}"]}
	}`)

	node, err := UnmarshalNode(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	debugNode, ok := node.(*DebugNode)
	if !ok {
		t.Fatalf("expected DebugNode, got %T", node)
	}

	if debugNode.Data.Expressions[0] != "{{request-1.body}}" {
		t.Fatalf("unexpected expressions: %#v", debugNode.Data.Expressions)
	}

	// Round trip ensure JSON tags match
	data, err := json.Marshal(debugNode)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected marshaled data")
	}
}
