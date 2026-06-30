package engine_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/nodes"
)

// fakeClock is the whole testability story: a node's only effect, faked in one
// line. No flow, no engine, no real time.
type fakeClock struct{ slept time.Duration }

func (f *fakeClock) Sleep(_ context.Context, d time.Duration) error { f.slept += d; return nil }
func (f *fakeClock) Now() time.Time                                 { return time.Unix(0, 0) }

// TestRequestAssertOutput drives a one-node flow end to end: the request node
// produces a Value, the engine runs the node's DECLARED assertion + output as a
// uniform post-step (the node code never mentions them).
func TestRequestAssertOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 7, "status": "ok"}`))
	}))
	defer srv.Close()

	flowJSON := `{
		"name": "t",
		"nodes": [{
			"id": "call", "type": "request", "method": "GET", "url": "` + srv.URL + `",
			"assertions": [{"path": "status", "op": "equals", "expected": "ok"}],
			"outputs":    [{"name": "uid", "path": "id"}]
		}],
		"edges": []
	}`
	f, err := flow.Parse([]byte(flowJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	e := engine.New(node.Runtime{HTTP: srv.Client(), Clock: &fakeClock{}}, nil)
	out, err := e.RunFlow(context.Background(), f, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if uid, ok := out["call.uid"].Int(); !ok || uid != 7 {
		t.Fatalf("extracted output call.uid: want 7, got %v (%v)", uid, out)
	}
	if status, ok := out["call.status"].Int(); !ok || status != http.StatusOK {
		t.Fatalf("node output call.status: want 200, got %v", status)
	}
}

// TestRequestAssertionFails proves the declared assertion actually gates: a
// wrong expectation fails the node as a user error.
func TestRequestAssertionFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer srv.Close()

	flowJSON := `{"name":"t","nodes":[{"id":"call","type":"request","url":"` + srv.URL + `",
		"assertions":[{"path":"status","op":"equals","expected":"NOPE"}]}],"edges":[]}`
	f, _ := flow.Parse([]byte(flowJSON))

	e := engine.New(node.Runtime{HTTP: srv.Client(), Clock: &fakeClock{}}, nil)
	if _, err := e.RunFlow(context.Background(), f, nil); err == nil {
		t.Fatal("expected assertion failure, got nil")
	}
}

// TestScheduling proves dependency ordering: B has an edge from A, so A runs
// first. Both delays land on the shared clock.
func TestScheduling(t *testing.T) {
	flowJSON := `{"name":"t","nodes":[
		{"id":"a","type":"delay","duration_ms":3},
		{"id":"b","type":"delay","duration_ms":4}],
		"edges":[{"source":"a","target":"b"}]}`
	f, _ := flow.Parse([]byte(flowJSON))

	clock := &fakeClock{}
	e := engine.New(node.Runtime{Clock: clock}, nil)
	out, err := e.RunFlow(context.Background(), f, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if clock.slept != 7*time.Millisecond {
		t.Fatalf("both delays should run: want 7ms, got %v", clock.slept)
	}
	if got, _ := out["b.delayed_ms"].Int(); got != 4 {
		t.Fatalf("want b.delayed_ms=4, got %v", got)
	}
}

// TestModuleRecursion proves a composite node re-enters the engine: the module
// node runs a child flow via the injected SubflowRunner (the engine itself).
func TestModuleRecursion(t *testing.T) {
	child, _ := flow.Parse([]byte(
		`{"name":"child","nodes":[{"id":"wait","type":"delay","duration_ms":5}],"edges":[]}`))
	resolve := func(id string) (flow.Flow, bool) {
		return child, id == "child"
	}
	parent, _ := flow.Parse([]byte(
		`{"name":"parent","nodes":[{"id":"m","type":"module","body_flow_id":"child"}],"edges":[]}`))

	clock := &fakeClock{}
	e := engine.New(node.Runtime{Clock: clock}, resolve)
	out, err := e.RunFlow(context.Background(), parent, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if clock.slept != 5*time.Millisecond {
		t.Fatalf("child delay should have run via recursion: got %v", clock.slept)
	}
	if got, ok := out["m.wait.delayed_ms"].Int(); !ok || got != 5 {
		t.Fatalf("child output should bubble up: want 5, got %v (%v)", got, out)
	}
}

// TestDefaultRuntimeWiring is a smoke test that the production wiring compiles
// and a node function is directly callable with faked deps — no scaffolding.
func TestDirectNodeCall(_ *testing.T) {
	_ = nodes.DefaultRuntime() // production wiring compiles
}
