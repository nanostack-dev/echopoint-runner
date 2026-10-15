package node_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// The control plane refuses a launch whose module flow does not exist, so a
// snapshot missing one is its fault and stays a runner-level error.
func TestModuleNode_MissingReferencedFlowIsNotUserError(t *testing.T) {
	n := &node.ModuleNode{
		BaseNode: node.BaseNode{ID: "module-1", NodeType: spi.KindModule},
		Data:     node.ModuleData{FlowID: "missing-flow"},
	}

	_, err := n.Execute(spi.ExecutionContext{ModuleExecutor: &fakePollExecutor{}})

	if err == nil {
		t.Fatal("expected a missing referenced flow to fail")
	}
	if _, ok := spi.AsUserError(err); ok {
		t.Fatalf("expected a runner fault, got UserError %v", err)
	}
}
