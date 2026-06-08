package runner_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
)

func TestMergeInputs_OverrideWinsBaseFills(t *testing.T) {
	merged := runner.MergeInputs(
		map[string]any{"a": 1, "b": 2},
		map[string]any{"b": 20, "c": 30},
	)
	if merged["a"] != 1 {
		t.Errorf("base value should fill: a=%v", merged["a"])
	}
	if merged["b"] != 20 {
		t.Errorf("override should win: b=%v", merged["b"])
	}
	if merged["c"] != 30 {
		t.Errorf("override-only key missing: c=%v", merged["c"])
	}
}

func TestModuleResolver_EmptyReturnsNil(t *testing.T) {
	if runner.ModuleResolver(nil) != nil {
		t.Error("no referenced flows must yield a nil resolver")
	}
	if runner.ModuleResolver(flow.ReferencedFlowRegistry{}) != nil {
		t.Error("empty referenced flows must yield a nil resolver")
	}
}

func TestModuleResolver_ResolvesRegisteredFlow(t *testing.T) {
	resolver := runner.ModuleResolver(flow.ReferencedFlowRegistry{
		"child": {FlowDefinition: []byte(`{"version":"1.0","name":"child","nodes":[],"edges":[]}`)},
	})
	if resolver == nil {
		t.Fatal("resolver should be non-nil when a flow is referenced")
	}
	if _, ok := resolver.ResolveFlow("child"); !ok {
		t.Error("registered flow id should resolve")
	}
	if _, ok := resolver.ResolveFlow("missing"); ok {
		t.Error("unknown flow id must not resolve")
	}
}

// Run is the single execution path for every transport: a minimal flow must
// execute through it and report success with a result per node.
func TestRun_ExecutesFlow(t *testing.T) {
	flowDef, err := flow.ParseFromJSON([]byte(
		`{"version":"1.0","name":"t","nodes":[{"id":"wait","type":"delay","data":{"duration":1}}],"edges":[]}`,
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := runner.Run(*flowDef, map[string]any{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Success {
		t.Errorf("flow should succeed, got error=%v", result.Error)
	}
	if _, ok := result.ExecutionResults["wait"]; !ok {
		t.Errorf("delay node result missing: %v", result.ExecutionResults)
	}
}
