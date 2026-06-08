package node_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

func TestUnmarshalNode_BuiltinsAndDefaults(t *testing.T) {
	n, err := node.UnmarshalNode([]byte(`{"id":"r","type":"request","data":{"method":"GET","url":"x"}}`))
	if err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if n.GetType() != node.TypeRequest {
		t.Errorf("expected request type, got %s", n.GetType())
	}
	if n.GetRunWhen() != node.RunWhenOnSuccess {
		t.Errorf("RunWhen should default to on_success, got %s", n.GetRunWhen())
	}

	if _, badErr := node.UnmarshalNode([]byte(`{"id":"x","type":"nope"}`)); badErr == nil {
		t.Error("unknown node type must error")
	}
}

func TestNewSkippedResult(t *testing.T) {
	base := node.BaseExecutionResult{NodeID: "n", NodeType: node.TypeRequest}
	if res, ok := node.NewSkippedResult(node.TypeRequest, base); !ok || res == nil {
		t.Error("request skipped result should build")
	}
	if _, ok := node.NewSkippedResult("nope", base); ok {
		t.Error("unknown type must not build a skipped result")
	}
}

func TestRegisterNodeKind_Custom(t *testing.T) {
	const custom node.Type = "custom-test-kind"
	node.RegisterNodeKind(custom,
		func(data []byte) (node.AnyNode, error) {
			var n node.DelayNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			return &n, nil
		},
		func(base node.BaseExecutionResult) node.AnyExecutionResult {
			return &node.DelayExecutionResult{BaseExecutionResult: base}
		},
	)

	n, err := node.UnmarshalNode([]byte(`{"id":"c","type":"custom-test-kind","data":{"duration":1}}`))
	if err != nil || n == nil {
		t.Fatalf("custom kind should decode: err=%v", err)
	}
	if _, ok := node.NewSkippedResult(custom, node.BaseExecutionResult{NodeID: "c"}); !ok {
		t.Error("custom kind skipped result should build")
	}
}
