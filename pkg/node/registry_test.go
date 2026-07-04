package node_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func TestUnmarshalNode_BuiltinsAndDefaults(t *testing.T) {
	n, err := node.UnmarshalNode([]byte(`{"id":"r","type":"request","data":{"method":"GET","url":"x"}}`))
	if err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if n.GetType() != spi.KindRequest {
		t.Errorf("expected request type, got %s", n.GetType())
	}
	if n.GetRunWhen() != spi.RunWhenOnSuccess {
		t.Errorf("spi.RunWhen should default to on_success, got %s", n.GetRunWhen())
	}

	if _, badErr := node.UnmarshalNode([]byte(`{"id":"x","type":"nope"}`)); badErr == nil {
		t.Error("unknown node type must error")
	}
}

func TestNewSkippedResult(t *testing.T) {
	base := spi.BaseExecutionResult{NodeID: "n", NodeType: spi.KindRequest}
	if res, ok := node.NewSkippedResult(spi.KindRequest, base); !ok || res == nil {
		t.Error("request skipped result should build")
	}
	if _, ok := node.NewSkippedResult("nope", base); ok {
		t.Error("unknown type must not build a skipped result")
	}
}

func TestRegisterNodeKind_Custom(t *testing.T) {
	const custom spi.Kind = "custom-test-kind"
	node.RegisterNodeKind(custom,
		func(data []byte) (node.AnyNode, error) {
			var n node.DelayNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &node.DelayExecutionResult{BaseExecutionResult: base}
		},
	)

	n, err := node.UnmarshalNode([]byte(`{"id":"c","type":"custom-test-kind","data":{"duration":1}}`))
	if err != nil || n == nil {
		t.Fatalf("custom kind should decode: err=%v", err)
	}
	if _, ok := node.NewSkippedResult(custom, spi.BaseExecutionResult{NodeID: "c"}); !ok {
		t.Error("custom kind skipped result should build")
	}
}
