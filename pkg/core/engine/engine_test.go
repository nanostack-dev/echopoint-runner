package engine_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/nodes"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/result"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// fakeClock is the whole testability story: a node's only effect, faked in one
// line. No flow, no engine, no real time.
type fakeClock struct{ slept time.Duration }

func (f *fakeClock) Sleep(_ context.Context, d time.Duration) error { f.slept += d; return nil }
func (f *fakeClock) Now() time.Time                                 { return time.Unix(0, 0) }

func parse(t *testing.T, flowJSON string) flow.Flow {
	t.Helper()
	f, err := flow.Parse([]byte(flowJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

// runOK runs a flow, requires it to succeed, and returns its nested outputs.
func runOK(t *testing.T, e *engine.Engine, f flow.Flow, inputs value.Map) value.Map {
	t.Helper()
	res := e.RunFlow(context.Background(), f, inputs)
	if !res.Success {
		t.Fatalf("flow should have succeeded; nodes=%+v", res.Nodes)
	}
	return res.Outputs
}

// runFail runs a flow, requires Success=false, and returns the full result.
func runFail(t *testing.T, e *engine.Engine, f flow.Flow, inputs value.Map) *result.FlowResult {
	t.Helper()
	res := e.RunFlow(context.Background(), f, inputs)
	if res.Success {
		t.Fatalf("flow should have failed; nodes=%+v", res.Nodes)
	}
	return res
}

// TestRequestAssertOutput drives a one-node flow end to end: the request node's
// declared assertion + output run as a uniform engine post-step.
func TestRequestAssertOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 7, "status": "ok"}`))
	}))
	defer srv.Close()

	f := parse(t, `{"name":"t","nodes":[{
		"id":"call","type":"request","method":"GET","url":"`+srv.URL+`",
		"assertions":[{"path":"body.status","op":"equals","expected":"ok"},
		              {"path":"status","op":"equals","expected":200}],
		"outputs":[{"name":"uid","path":"body.id"}]}],"edges":[]}`)

	out := runOK(t, engine.New(node.Runtime{HTTP: srv.Client(), Clock: &fakeClock{}}, nil), f, nil)
	uidV, _ := out["call"].Get("uid")
	if uid, ok := uidV.Int(); !ok || uid != 7 {
		t.Fatalf("call.uid: want 7, got %v", uid)
	}
	statusV, _ := out["call"].Get("status")
	if status, ok := statusV.Int(); !ok || status != http.StatusOK {
		t.Fatalf("call.status: want 200, got %v", status)
	}
}

// TestRequestAssertionFails proves a wrong expectation fails the node with code
// ASSERTION_FAILED and records the assertion outcome.
func TestRequestAssertionFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer srv.Close()

	f := parse(t, `{"name":"t","nodes":[{"id":"call","type":"request","url":"`+srv.URL+`",
		"assertions":[{"path":"body.status","op":"equals","expected":"NOPE"}]}],"edges":[]}`)
	res := runFail(t, engine.New(node.Runtime{HTTP: srv.Client(), Clock: &fakeClock{}}, nil), f, nil)
	if got := res.Nodes["call"].Code; got != "ASSERTION_FAILED" {
		t.Fatalf("want code ASSERTION_FAILED, got %q", got)
	}
	if len(res.Nodes["call"].Assertions) == 0 {
		t.Fatal("assertion results should be recorded on the failed node")
	}
}

// TestScheduling proves dependency ordering: B waits for A via an edge.
func TestScheduling(t *testing.T) {
	f := parse(t, `{"name":"t","nodes":[
		{"id":"a","type":"delay","duration_ms":3},
		{"id":"b","type":"delay","duration_ms":4}],
		"edges":[{"source":"a","target":"b"}]}`)
	clock := &fakeClock{}
	out := runOK(t, engine.New(node.Runtime{Clock: clock}, nil), f, nil)
	if clock.slept != 7*time.Millisecond {
		t.Fatalf("both delays should run: want 7ms, got %v", clock.slept)
	}
	bV, _ := out["b"].Get("delayed_ms")
	if got, _ := bV.Int(); got != 4 {
		t.Fatalf("want b.delayed_ms=4, got %v", got)
	}
}

// TestModuleRecursion proves a composite node re-enters the engine.
func TestModuleRecursion(t *testing.T) {
	child := parse(t, `{"name":"child","nodes":[{"id":"wait","type":"delay","duration_ms":5}],"edges":[]}`)
	resolve := func(id string) (flow.Flow, bool) { return child, id == "child" }
	parent := parse(t, `{"name":"parent","nodes":[{"id":"m","type":"module","body_flow_id":"child"}],"edges":[]}`)

	clock := &fakeClock{}
	out := runOK(t, engine.New(node.Runtime{Clock: clock}, resolve), parent, nil)
	if clock.slept != 5*time.Millisecond {
		t.Fatalf("child delay should have run via recursion: got %v", clock.slept)
	}
	wV, _ := out["m"].Get("wait.delayed_ms")
	if got, ok := wV.Int(); !ok || got != 5 {
		t.Fatalf("child output should bubble up: want 5, got %v", got)
	}
}

// TestInterNodeTemplating proves node B reads node A's output via {{login.token}}.
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

	f := parse(t, `{"name":"auth","nodes":[
		{"id":"login","type":"request","method":"POST","url":"`+login.URL+`",
		 "outputs":[{"name":"token","path":"body.access_token"}]},
		{"id":"me","type":"request","method":"GET","url":"`+me.URL+`",
		 "headers":{"Authorization":"Bearer {{login.token}}"},
		 "assertions":[{"path":"body.ok","op":"equals","expected":true}]}],
		"edges":[{"source":"login","target":"me"}]}`)
	runOK(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)
}

// TestPollUntil proves poll re-runs an inline body until its exit condition holds.
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

	f := parse(t, `{"name":"p","nodes":[{
		"id":"poll","type":"poll","interval_ms":1,"max_attempts":5,
		"body":{"nodes":[{"id":"check","type":"request","url":"`+srv.URL+`",
			"outputs":[{"name":"status","path":"body.status"}]}],"edges":[]},
		"assertions":[{"path":"check.status","op":"equals","expected":"done"}]}],"edges":[]}`)
	out := runOK(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)
	attV, _ := out["poll"].Get("attempts")
	if a, ok := attV.Int(); !ok || a != 3 {
		t.Fatalf("want 3 attempts, got %v", a)
	}
	if calls != 3 {
		t.Fatalf("server hit %d times, want 3", calls)
	}
}

// TestPollExhausts proves poll fails when the condition never holds.
func TestPollExhausts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer srv.Close()

	f := parse(t, `{"name":"p","nodes":[{
		"id":"poll","type":"poll","interval_ms":1,"max_attempts":2,
		"body":{"nodes":[{"id":"check","type":"request","url":"`+srv.URL+`",
			"outputs":[{"name":"status","path":"body.status"}]}],"edges":[]},
		"assertions":[{"path":"check.status","op":"equals","expected":"done"}]}],"edges":[]}`)
	runFail(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)
}

// TestSetVariable proves computed variables (string concat + {{{raw}}}).
func TestSetVariable(t *testing.T) {
	f := parse(t, `{"name":"sv","nodes":[{
		"id":"vars","type":"set_variable",
		"variables":{"greeting":"hi {{a}}","n":"{{{count}}}"},
		"assertions":[{"path":"greeting","op":"equals","expected":"hi x"}],
		"outputs":[{"name":"g","path":"greeting"}]}],"edges":[]}`)
	out := runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f,
		value.Map{"a": value.Of("x"), "count": value.Of(5)})
	gV, _ := out["vars"].Get("g")
	if g, _ := gV.Str(); g != "hi x" {
		t.Fatalf("want g='hi x', got %q", g)
	}
	nV, _ := out["vars"].Get("n")
	if n, ok := nV.Int(); !ok || n != 5 {
		t.Fatalf("want n=5 (raw passthrough), got %v", n)
	}
}

// TestAssertOverInputs proves the target-less assert over flow inputs.
func TestAssertOverInputs(t *testing.T) {
	f := parse(t, `{"name":"a","nodes":[{"id":"check","type":"assert",
		"assertions":[{"path":"role","op":"equals","expected":"admin"}]}],"edges":[]}`)
	e := engine.New(node.Runtime{Clock: &fakeClock{}}, nil)
	runOK(t, e, f, value.Map{"role": value.Of("admin")})
	runFail(t, e, f, value.Map{"role": value.Of("guest")})
}

// TestLoopForeach proves the foreach loop + aggregate assertion.
func TestLoopForeach(t *testing.T) {
	f := parse(t, `{"name":"l","nodes":[{
		"id":"each","type":"loop","items":"{{{nums}}}","item_var":"n",
		"body":{"nodes":[{"id":"echo","type":"set_variable","variables":{"v":"{{n}}"}}],"edges":[]},
		"assertions":[{"path":"count","op":"equals","expected":3}]}],"edges":[]}`)
	out := runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f,
		value.Map{"nums": value.Of([]any{10, 20, 30})})
	cV, _ := out["each"].Get("count")
	if c, ok := cV.Int(); !ok || c != 3 {
		t.Fatalf("want count=3, got %v", c)
	}
}

// TestLoopNonListErrors proves non-list items fail.
func TestLoopNonListErrors(t *testing.T) {
	f := parse(t, `{"name":"l","nodes":[{"id":"each","type":"loop","items":"{{{notalist}}}",
		"body":{"nodes":[{"id":"echo","type":"set_variable","variables":{"v":"x"}}],"edges":[]}}],"edges":[]}`)
	runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"notalist": value.Of("scalar")})
}

// TestBranchRoutes proves routing: branch picks A, B is skipped.
func TestBranchRoutes(t *testing.T) {
	f := parse(t, `{"name":"b","nodes":[
		{"id":"route","type":"branch","cases":[
			{"when":{"path":"x","op":"equals","expected":"a"},"target":"A"},
			{"when":{"path":"x","op":"equals","expected":"b"},"target":"B"}]},
		{"id":"A","type":"set_variable","variables":{"hit":"A"}},
		{"id":"B","type":"set_variable","variables":{"hit":"B"}}],
		"edges":[{"source":"route","target":"A"},{"source":"route","target":"B"}]}`)
	res := runFailOrOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"x": value.Of("a")})
	if res.Nodes["A"].Status != result.StatusSuccess {
		t.Fatal("A should have run")
	}
	if res.Nodes["B"].Status != result.StatusSkipped || res.Nodes["B"].SkipReason != result.SkipRoutedAway {
		t.Fatalf("B should be skipped routed_away_by_branch, got %+v", res.Nodes["B"])
	}
}

// TestBranchSkipCascade proves the skip cascades to B's child B1.
func TestBranchSkipCascade(t *testing.T) {
	f := parse(t, `{"name":"b","nodes":[
		{"id":"route","type":"branch","default":"A",
		 "cases":[{"when":{"path":"go","op":"equals","expected":"yes"},"target":"A"}]},
		{"id":"A","type":"set_variable","variables":{"v":"a"}},
		{"id":"B","type":"set_variable","variables":{"v":"b"}},
		{"id":"B1","type":"set_variable","variables":{"v":"b1"}}],
		"edges":[{"source":"route","target":"A"},{"source":"route","target":"B"},
		         {"source":"B","target":"B1"}]}`)
	res := runFailOrOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"go": value.Of("no")})
	if res.Nodes["A"].Status != result.StatusSuccess {
		t.Fatal("A (default) should have run")
	}
	if res.Nodes["B"].Status != result.StatusSkipped || res.Nodes["B1"].Status != result.StatusSkipped {
		t.Fatalf("B and B1 should be skipped, got B=%+v B1=%+v", res.Nodes["B"], res.Nodes["B1"])
	}
}

// TestSseCollectsEvents proves the SSE node parses a stream to EOF.
func TestSseCollectsEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": comment\n\ndata: {\"n\":0}\n\ndata: {\"n\":1}\n\ndata: {\"n\":2}\n\n"))
	}))
	defer srv.Close()

	f := parse(t, `{"name":"s","nodes":[{"id":"stream","type":"sse","url":"`+srv.URL+`","max_events":10,
		"assertions":[{"path":"n","op":"gte","expected":0}]}],"edges":[]}`)
	out := runOK(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)
	cV, _ := out["stream"].Get("count")
	if c, ok := cV.Int(); !ok || c != 3 {
		t.Fatalf("want 3 events, got %v", c)
	}
}

// TestSseNon2xxFails proves a non-2xx connect fails.
func TestSseNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	f := parse(t, `{"name":"s","nodes":[{"id":"stream","type":"sse","url":"`+srv.URL+`"}],"edges":[]}`)
	runFail(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)
}

// TestAssertCrossNode proves a target-less assert referencing a prior node.
func TestAssertCrossNode(t *testing.T) {
	f := parse(t, `{"name":"x","nodes":[
		{"id":"vars","type":"set_variable","variables":{"count":"{{{n}}}"}},
		{"id":"check","type":"assert","assertions":[{"path":"vars.count","op":"equals","expected":5}]}],
		"edges":[{"source":"vars","target":"check"}]}`)
	runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"n": value.Of(5)})
}

// TestAssertUnknownRefFails proves the reference guard.
func TestAssertUnknownRefFails(t *testing.T) {
	f := parse(t, `{"name":"x","nodes":[
		{"id":"check","type":"assert","assertions":[{"path":"typo.field","op":"equals","expected":"x"}]}],"edges":[]}`)
	res := runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, nil)
	if got := res.Nodes["check"].Code; got != "UNKNOWN_REFERENCE" {
		t.Fatalf("want UNKNOWN_REFERENCE, got %q", got)
	}
}

// TestFailureSkipsDependents proves the result model: a failed node is recorded
// with its code, its dependent is skipped with reason dependency_failed, the run
// continues, and the flow reports Success=false.
func TestFailureSkipsDependents(t *testing.T) {
	f := parse(t, `{"name":"f","nodes":[
		{"id":"a","type":"assert","assertions":[{"path":"ok","op":"equals","expected":"yes"}]},
		{"id":"b","type":"set_variable","variables":{"v":"1"}}],
		"edges":[{"source":"a","target":"b"}]}`)
	res := runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"ok": value.Of("no")})
	if res.Nodes["a"].Status != result.StatusFailed || res.Nodes["a"].Code != "ASSERTION_FAILED" {
		t.Fatalf("a should fail ASSERTION_FAILED, got %+v", res.Nodes["a"])
	}
	if res.Nodes["b"].Status != result.StatusSkipped || res.Nodes["b"].SkipReason != result.SkipDependencyFailed {
		t.Fatalf("b should be skipped dependency_failed, got %+v", res.Nodes["b"])
	}
}

// TestAlwaysCleanupRunsAfterFailure proves run_when=always cleanup still runs
// after a main-phase failure.
func TestAlwaysCleanupRunsAfterFailure(t *testing.T) {
	f := parse(t, `{"name":"f","nodes":[
		{"id":"a","type":"assert","assertions":[{"path":"ok","op":"equals","expected":"yes"}]},
		{"id":"cleanup","type":"set_variable","run_when":"always","variables":{"done":"1"}}],
		"edges":[]}`)
	res := runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"ok": value.Of("no")})
	if res.Nodes["a"].Status != result.StatusFailed {
		t.Fatalf("a should fail, got %+v", res.Nodes["a"])
	}
	if res.Nodes["cleanup"].Status != result.StatusSuccess {
		t.Fatalf("always cleanup should run after main failure, got %+v", res.Nodes["cleanup"])
	}
}

// runFailOrOK runs a flow tolerating either verdict (used where skips, not
// failures, are the point) and returns the full result.
func runFailOrOK(_ *testing.T, e *engine.Engine, f flow.Flow, inputs value.Map) *result.FlowResult {
	return e.RunFlow(context.Background(), f, inputs)
}

// fakeVars is a deterministic dynamic-variable resolver for tests.
type fakeVars struct{}

func (fakeVars) Resolve(name string, _ []string) (string, error) {
	if name == "greeting" {
		return "hello", nil
	}
	return "", errors.New("unknown dynamic variable")
}

// TestDynamicVars proves {{$name}} resolves via the runtime's resolver during
// templating, before the node decodes.
func TestDynamicVars(t *testing.T) {
	f := parse(t, `{"name":"d","nodes":[{"id":"vars","type":"set_variable",
		"variables":{"g":"{{$greeting}}!"},
		"assertions":[{"path":"g","op":"equals","expected":"hello!"}]}],"edges":[]}`)
	e := engine.New(node.Runtime{Clock: &fakeClock{}, Vars: fakeVars{}}, nil)
	runOK(t, e, f, nil)
}

// TestValidationBranchTargetNoEdge proves a branch routing to a target with no
// edge fails validation before execution.
func TestValidationBranchTargetNoEdge(t *testing.T) {
	f := parse(t, `{"name":"v","nodes":[
		{"id":"route","type":"branch","cases":[{"when":{"path":"a","op":"equals","expected":"1"},"target":"X"}]},
		{"id":"A","type":"set_variable","variables":{"v":"a"}}],
		"edges":[{"source":"route","target":"A"}]}`)
	res := runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"a": value.Of("1")})
	if res.Code != "FLOW_VALIDATION_FAILED" {
		t.Fatalf("want FLOW_VALIDATION_FAILED, got %q", res.Code)
	}
}

// TestValidationEdgeToUnknownNode proves an edge to a non-existent node fails
// validation.
func TestValidationEdgeToUnknownNode(t *testing.T) {
	f := parse(t, `{"name":"v","nodes":[{"id":"a","type":"delay","duration_ms":1}],
		"edges":[{"source":"a","target":"ghost"}]}`)
	res := runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, nil)
	if res.Code != "FLOW_VALIDATION_FAILED" {
		t.Fatalf("want FLOW_VALIDATION_FAILED, got %q", res.Code)
	}
}

// TestJSONPathFilter proves real RFC-9535 jsonpath: a filter selects an array
// element by predicate.
func TestJSONPathFilter(t *testing.T) {
	f := parse(t, `{"name":"jp","nodes":[{"id":"check","type":"assert",
		"assertions":[{"path":"$.users[?@.role=='admin'].name","op":"equals","expected":"alice"}]}],"edges":[]}`)
	inputs := value.Map{"users": value.Of([]any{
		map[string]any{"name": "alice", "role": "admin"},
		map[string]any{"name": "bob", "role": "user"},
	})}
	runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, inputs)
}

// TestObserverEvents proves the engine emits the expected event sequence for a
// two-node chain: flow.started, then per node started+completed, then
// flow.completed.
func TestObserverEvents(t *testing.T) {
	f := parse(t, `{"name":"o","nodes":[
		{"id":"a","type":"delay","duration_ms":1},
		{"id":"b","type":"delay","duration_ms":1}],
		"edges":[{"source":"a","target":"b"}]}`)
	var types []spi.EventType
	obs := func(ev engine.Event) { types = append(types, ev.Type) }
	e := engine.New(node.Runtime{Clock: &fakeClock{}}, nil, engine.WithObserver(obs))
	runOK(t, e, f, nil)

	want := []spi.EventType{
		spi.EventFlowStarted,
		spi.EventNodeStarted, spi.EventNodeCompleted, // a
		spi.EventNodeStarted, spi.EventNodeCompleted, // b
		spi.EventFlowCompleted,
	}
	if len(types) != len(want) {
		t.Fatalf("want %d events, got %d: %v", len(want), len(types), types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("event %d: want %q got %q (%v)", i, want[i], types[i], types)
		}
	}
}

// TestRetryMiddleware proves Retry re-runs a node (re-request + re-assert) until
// it passes: a flaky server reports "pending" then "ready".
func TestRetryMiddleware(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		status := "pending"
		if calls >= 2 {
			status = "ready"
		}
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	}))
	defer srv.Close()

	f := parse(t, `{"name":"r","nodes":[{"id":"call","type":"request","url":"`+srv.URL+`",
		"assertions":[{"path":"body.status","op":"equals","expected":"ready"}]}],"edges":[]}`)

	// no retry -> first attempt is "pending" -> fails
	runFail(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)

	// with Retry(3) -> succeeds on the 2nd attempt
	calls = 0
	e := engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil,
		engine.WithMiddleware(engine.Retry(3)))
	runOK(t, e, f, nil)
	if calls != 2 {
		t.Fatalf("want 2 calls (retry passed on 2nd), got %d", calls)
	}
}

// TestTimeoutMiddleware proves Timeout bounds a node: a 50ms delay under a 1ms
// per-node timeout is cancelled and the node fails.
func TestTimeoutMiddleware(t *testing.T) {
	f := parse(t, `{"name":"to","nodes":[{"id":"wait","type":"delay","duration_ms":50}],"edges":[]}`)
	e := engine.New(node.Runtime{Clock: nodes.WallClock{}}, nil,
		engine.WithMiddleware(engine.Timeout(time.Millisecond)))
	runFail(t, e, f, nil)
}

// TestModuleCycleUpfront proves an A->B->A module cycle is caught before any
// node runs (no side effects).
func TestModuleCycleUpfront(t *testing.T) {
	a := parse(t, `{"name":"a","nodes":[{"id":"m","type":"module","body_flow_id":"B"}],"edges":[]}`)
	b := parse(t, `{"name":"b","nodes":[{"id":"m","type":"module","body_flow_id":"A"}],"edges":[]}`)
	resolve := func(id string) (flow.Flow, bool) {
		switch id {
		case "A":
			return a, true
		case "B":
			return b, true
		}
		return flow.Flow{}, false
	}
	res := runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, resolve), a, nil)
	if res.Code != "FLOW_VALIDATION_FAILED" {
		t.Fatalf("want FLOW_VALIDATION_FAILED, got %q", res.Code)
	}
	if len(res.Nodes) != 0 {
		t.Fatalf("no node should run on a validation failure, got %v", res.Nodes)
	}
}

// TestModuleUnknownFlow proves a module referencing an unresolvable flow fails
// validation upfront.
func TestModuleUnknownFlow(t *testing.T) {
	a := parse(t, `{"name":"a","nodes":[{"id":"m","type":"module","body_flow_id":"ghost"}],"edges":[]}`)
	resolve := func(string) (flow.Flow, bool) { return flow.Flow{}, false }
	res := runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, resolve), a, nil)
	if res.Code != "FLOW_VALIDATION_FAILED" {
		t.Fatalf("want FLOW_VALIDATION_FAILED, got %q", res.Code)
	}
}

// TestParallelJoin proves a diamond join: c has two predecessors, runs once
// after both, and reads both their outputs.
func TestParallelJoin(t *testing.T) {
	f := parse(t, `{"name":"d","nodes":[
		{"id":"a","type":"set_variable","variables":{"v":"A"}},
		{"id":"b","type":"set_variable","variables":{"v":"B"}},
		{"id":"c","type":"set_variable","variables":{"got":"{{a.v}}-{{b.v}}"}}],
		"edges":[{"source":"a","target":"c"},{"source":"b","target":"c"}]}`)
	out := runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, nil)
	gv, _ := out["c"].Get("got")
	if g, _ := gv.Str(); g != "A-B" {
		t.Fatalf("join should see both inputs: got %q", g)
	}
}

// TestLoopContinueOnError proves a failing iteration is captured and the loop
// continues when continue_on_error is set.
func TestLoopContinueOnError(t *testing.T) {
	f := parse(t, `{"name":"l","nodes":[{
		"id":"each","type":"loop","items":"{{{nums}}}","item_var":"item","continue_on_error":true,
		"body":{"nodes":[{"id":"chk","type":"assert","assertions":[{"path":"item","op":"not_equals","expected":2}]}],"edges":[]},
		"assertions":[{"path":"count","op":"equals","expected":3}]}],"edges":[]}`)
	out := runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f,
		value.Map{"nums": value.Of([]any{1, 2, 3})})
	cV, _ := out["each"].Get("count")
	if c, _ := cV.Int(); c != 3 {
		t.Fatalf("all 3 iterations should be recorded, got count=%v", c)
	}
}

// TestPollBodyError proves a body that errors (not merely unsatisfied) fails the
// poll with POLL_BODY_FAILED.
func TestPollBodyError(t *testing.T) {
	f := parse(t, `{"name":"p","nodes":[{
		"id":"poll","type":"poll","interval_ms":1,"max_attempts":3,
		"body":{"nodes":[{"id":"bad","type":"assert","assertions":[{"path":"ghost.x","op":"equals","expected":1}]}],"edges":[]},
		"assertions":[{"path":"bad.ok","op":"equals","expected":true}]}],"edges":[]}`)
	res := runFail(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, nil)
	if got := res.Nodes["poll"].Code; got != "POLL_BODY_FAILED" {
		t.Fatalf("want POLL_BODY_FAILED, got %q", got)
	}
}

// TestSseCompletionEvent proves the stream stops on a named completion event.
func TestSseCompletionEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"n\":0}\n\nevent: done\ndata: bye\n\ndata: {\"n\":1}\n\n"))
	}))
	defer srv.Close()
	f := parse(
		t,
		`{"name":"s","nodes":[{"id":"stream","type":"sse","url":"`+srv.URL+`","completion_event":"done"}],"edges":[]}`,
	)
	out := runOK(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)
	srV, _ := out["stream"].Get("stop_reason")
	if s, _ := srV.Str(); s != "completion_event" {
		t.Fatalf("stop_reason=%q, want completion_event", s)
	}
	cV, _ := out["stream"].Get("count")
	if c, _ := cV.Int(); c != 2 {
		t.Fatalf("want 2 events (n:0 + done), got %v", c)
	}
}

// TestSseMaxEvents proves the stream stops after max_events.
func TestSseMaxEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: 0\n\ndata: 1\n\ndata: 2\n\ndata: 3\n\n"))
	}))
	defer srv.Close()
	f := parse(t, `{"name":"s","nodes":[{"id":"stream","type":"sse","url":"`+srv.URL+`","max_events":2}],"edges":[]}`)
	out := runOK(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)
	srV, _ := out["stream"].Get("stop_reason")
	if s, _ := srV.Str(); s != "max_events" {
		t.Fatalf("stop_reason=%q, want max_events", s)
	}
}

// TestHyphenatedRefs proves hyphenated node ids and header keys are addressable:
// a "create-user" node, a "content-type" header, a "user-id" body field, and a
// cross-node ref to a hyphenated id all resolve (regression for the RFC-9535
// dot-shorthand hyphen bug).
func TestHyphenatedRefs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user-id": 42}`))
	}))
	defer srv.Close()

	f := parse(t, `{"name":"h","nodes":[
		{"id":"create-user","type":"request","url":"`+srv.URL+`",
		 "assertions":[{"path":"headers.content-type","op":"contains","expected":"json"},
		               {"path":"body.user-id","op":"equals","expected":42}],
		 "outputs":[{"name":"uid","path":"body.user-id"}]},
		{"id":"check","type":"assert","assertions":[{"path":"create-user.uid","op":"equals","expected":42}]}],
		"edges":[{"source":"create-user","target":"check"}]}`)
	runOK(t, engine.New(node.Runtime{HTTP: srv.Client(), Clock: &fakeClock{}}, nil), f, nil)
}

// TestDeepTemplateRef proves a template ref reaches a node's nested output
// ({{call.body.ok}}), not just its top-level declared outputs — templates and
// assertions share one path addressing.
func TestDeepTemplateRef(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	f := parse(t, `{"name":"d","nodes":[
		{"id":"call","type":"request","url":"`+srv.URL+`"},
		{"id":"echo","type":"set_variable","variables":{"got":"{{call.body.ok}}"},
		 "assertions":[{"path":"got","op":"equals","expected":"true"}]}],
		"edges":[{"source":"call","target":"echo"}]}`)
	runOK(t, engine.New(node.Runtime{HTTP: srv.Client(), Clock: &fakeClock{}}, nil), f, nil)
}

// TestModuleInputs proves a module passes its inputs into the child flow.
func TestModuleInputs(t *testing.T) {
	child := parse(t, `{"name":"child","nodes":[{"id":"echo","type":"set_variable",
		"variables":{"seen":"{{who}}"},"assertions":[{"path":"seen","op":"equals","expected":"alice"}]}],"edges":[]}`)
	resolve := func(id string) (flow.Flow, bool) { return child, id == "child" }
	parent := parse(t, `{"name":"parent","nodes":[{"id":"m","type":"module",
		"body_flow_id":"child","inputs":{"who":"alice"}}],"edges":[]}`)
	runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, resolve), parent, nil)
}

// TestFlowDefaultInputs proves a flow's declared "inputs" act as defaults that
// the passed inputs override.
func TestFlowDefaultInputs(t *testing.T) {
	f := parse(t, `{"name":"d","inputs":{"env":"prod","region":"us"},
		"nodes":[{"id":"v","type":"set_variable","variables":{"e":"{{env}}","r":"{{region}}"},
		"assertions":[{"path":"e","op":"equals","expected":"dev"},
		              {"path":"r","op":"equals","expected":"us"}]}],"edges":[]}`)
	runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"env": value.Of("dev")})
}

// TestFailedNodeKeepsOutputs proves a node that fails an assertion still surfaces
// its produced outputs (so a UI can show the body that failed).
func TestFailedNodeKeepsOutputs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":9,"status":"bad"}`))
	}))
	defer srv.Close()
	f := parse(t, `{"name":"f","nodes":[{"id":"call","type":"request","url":"`+srv.URL+`",
		"assertions":[{"path":"body.status","op":"equals","expected":"ok"}],
		"outputs":[{"name":"uid","path":"body.id"}]}],"edges":[]}`)
	res := runFail(t, engine.New(node.Runtime{HTTP: srv.Client(), Clock: &fakeClock{}}, nil), f, nil)
	uidV, ok := res.Nodes["call"].Outputs["uid"]
	if !ok {
		t.Fatal("failed node should keep its extracted outputs")
	}
	if i, _ := uidV.Int(); i != 9 {
		t.Fatalf("uid=%v", i)
	}
}

// TestCancellation proves a cancelled context stops the flow with CANCELLED.
func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := parse(t, `{"name":"c","nodes":[{"id":"a","type":"delay","duration_ms":1}],"edges":[]}`)
	res := engine.New(node.Runtime{Clock: &fakeClock{}}, nil).RunFlow(ctx, f, nil)
	if res.Success || res.Code != "CANCELLED" {
		t.Fatalf("cancelled flow: want CANCELLED, got success=%v code=%q", res.Success, res.Code)
	}
}

// TestAlwaysNodeFailureDoesNotFailFlow locks the contract: an always-cleanup
// node that itself fails does not fail the flow.
func TestAlwaysNodeFailureDoesNotFailFlow(t *testing.T) {
	f := parse(t, `{"name":"a","nodes":[
		{"id":"main","type":"set_variable","variables":{"v":"1"}},
		{"id":"cleanup","type":"assert","run_when":"always",
		 "assertions":[{"path":"missing","op":"equals","expected":1}]}],"edges":[]}`)
	res := runFailOrOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, nil)
	if !res.Success {
		t.Fatal("an always-node failure should NOT fail the flow")
	}
	if res.Nodes["cleanup"].Status != result.StatusFailed {
		t.Fatalf("cleanup should be recorded failed, got %+v", res.Nodes["cleanup"])
	}
}

// TestLoopMaxIterations proves max_iterations caps the loop.
func TestLoopMaxIterations(t *testing.T) {
	f := parse(t, `{"name":"l","nodes":[{"id":"each","type":"loop","items":"{{{nums}}}","max_iterations":2,
		"body":{"nodes":[{"id":"e","type":"set_variable","variables":{"v":"{{item}}"}}],"edges":[]},
		"assertions":[{"path":"count","op":"equals","expected":2}]}],"edges":[]}`)
	runOK(t, engine.New(node.Runtime{Clock: &fakeClock{}}, nil), f, value.Map{"nums": value.Of([]any{1, 2, 3, 4, 5})})
}

// TestSseContinuesOnAssertionFailure proves stop_on_assertion_failure=false keeps
// consuming past a failing event to EOF.
func TestSseContinuesOnAssertionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"n\":0}\n\ndata: {\"n\":1}\n\ndata: {\"n\":2}\n\n"))
	}))
	defer srv.Close()
	f := parse(t, `{"name":"s","nodes":[{"id":"stream","type":"sse","url":"`+srv.URL+`",
		"stop_on_assertion_failure":false,
		"assertions":[{"path":"n","op":"lt","expected":1}]}],"edges":[]}`)
	out := runOK(t, engine.New(node.Runtime{HTTP: http.DefaultClient, Clock: &fakeClock{}}, nil), f, nil)
	cV, _ := out["stream"].Get("count")
	if c, _ := cV.Int(); c != 3 {
		t.Fatalf("want 3 (continued past failing events), got %v", c)
	}
}

// TestDirectNodeCall is a smoke test that the production wiring compiles.
func TestDirectNodeCall(_ *testing.T) {
	_ = nodes.DefaultRuntime()
}
