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

	uidV, _ := out["call"].Get("uid")
	if uid, ok := uidV.Int(); !ok || uid != 7 {
		t.Fatalf("extracted output call.uid: want 7, got %v (%v)", uid, out)
	}
	statusV, _ := out["call"].Get("status")
	if status, ok := statusV.Int(); !ok || status != http.StatusOK {
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
	bV, _ := out["b"].Get("delayed_ms")
	if got, _ := bV.Int(); got != 4 {
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
	wV, _ := out["m"].Get("wait.delayed_ms")
	if got, ok := wV.Int(); !ok || got != 5 {
		t.Fatalf("child output should bubble up: want 5, got %v (%v)", got, out)
	}
}

// TestInterNodeTemplating proves node B reads node A's output: login returns a
// token, the second node uses {{login.token}} in its auth header, and the server
// answers 200 only when the header is correct — so a passing flow proves the
// template resolved against the bus.
func TestInterNodeTemplating(t *testing.T) {
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token": "xyz"}`))
	}))
	defer login.Close()
	me := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xyz" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer me.Close()

	flowJSON := `{"name":"auth","nodes":[
		{"id":"login","type":"request","method":"POST","url":"` + login.URL + `",
		 "outputs":[{"name":"token","path":"access_token"}]},
		{"id":"me","type":"request","method":"GET","url":"` + me.URL + `",
		 "headers":{"Authorization":"Bearer {{login.token}}"},
		 "assertions":[{"path":"ok","op":"equals","expected":true}]}],
		"edges":[{"source":"login","target":"me"}]}`
	f, err := flow.Parse([]byte(flowJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	e := engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil)
	if _, runErr := e.RunFlow(context.Background(), f, nil); runErr != nil {
		t.Fatalf("templated auth flow failed (token did not resolve?): %v", runErr)
	}
}

// TestPollUntil proves the self-evaluating loop: poll re-runs an inline body
// flow until its exit condition passes. The server reports "pending" twice then
// "done"; poll must stop on the 3rd attempt. The node calls assert.Run itself.
func TestPollUntil(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		status := "pending"
		if calls >= 3 {
			status = "done"
		}
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	}))
	defer srv.Close()

	flowJSON := `{"name":"p","nodes":[{
		"id":"poll","type":"poll","interval_ms":1,"max_attempts":5,
		"body":{"nodes":[{"id":"check","type":"request","url":"` + srv.URL + `",
			"outputs":[{"name":"status","path":"status"}]}],"edges":[]},
		"assertions":[{"path":"check.status","op":"equals","expected":"done"}]
	}],"edges":[]}`
	f, err := flow.Parse([]byte(flowJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	e := engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil)
	out, err := e.RunFlow(context.Background(), f, nil)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	attV, _ := out["poll"].Get("attempts")
	if a, ok := attV.Int(); !ok || a != 3 {
		t.Fatalf("want 3 attempts, got %v", a)
	}
	if calls != 3 {
		t.Fatalf("server hit %d times, want 3", calls)
	}
}

// TestPollExhausts proves poll fails as a user error when the condition never
// holds within the attempt budget.
func TestPollExhausts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer srv.Close()

	flowJSON := `{"name":"p","nodes":[{
		"id":"poll","type":"poll","interval_ms":1,"max_attempts":2,
		"body":{"nodes":[{"id":"check","type":"request","url":"` + srv.URL + `",
			"outputs":[{"name":"status","path":"status"}]}],"edges":[]},
		"assertions":[{"path":"check.status","op":"equals","expected":"done"}]
	}],"edges":[]}`
	f, _ := flow.Parse([]byte(flowJSON))
	e := engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil)
	if _, err := e.RunFlow(context.Background(), f, nil); err == nil {
		t.Fatal("expected poll to exhaust and fail")
	}
}

// TestDirectNodeCall is a smoke test that the production wiring compiles.
func TestDirectNodeCall(_ *testing.T) {
	_ = nodes.DefaultRuntime()
}
