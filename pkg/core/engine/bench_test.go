package engine_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

// --- fake transport: canned responses, zero latency -------------------------
//
// benchmarks isolate ENGINE overhead (schedule, template, assert, recurse) from
// network time. instantDoer implements node.HTTPDoer and returns a fresh canned
// response per call — no sockets. A URL path of "/sse" yields an event-stream.

const cannedJSON = `{"val":42,"access_token":"xyz","nested":{"deep":{"id":"abc"}}}`

type instantDoer struct{ sseEvents int }

func (d instantDoer) Do(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/sse") {
		var b strings.Builder
		for i := range d.sseEvents {
			fmt.Fprintf(&b, "data: {\"n\":%d}\n\n", i)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(b.String())),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(cannedJSON)),
	}, nil
}

func benchRuntime(doer node.HTTPDoer) node.Runtime {
	return node.Runtime{HTTP: doer, Clock: &fakeClock{}}
}

// mustParse fails the benchmark loudly on a malformed generated flow.
func mustParse(b *testing.B, j string) flow.Flow {
	b.Helper()
	f, err := flow.Parse([]byte(j))
	if err != nil {
		b.Fatalf("bad generated flow: %v\n%s", err, j)
	}
	return f
}

// runBench runs the flow N times and fails if any run is unsuccessful, so a
// benchmark can never silently measure a validation-error fast path.
func runBench(b *testing.B, e *engine.Engine, f flow.Flow) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		res := e.RunFlow(context.Background(), f, value.Map{})
		if !res.Success {
			b.Fatalf("flow failed: %s (%s)", res.Error, res.Code)
		}
	}
}

// --- flow generators (stress graphs) ----------------------------------------

// genWideDiamond: root -> N parallel requests -> a sink assert that references
// every parallel node. The sink's input view spans all N upstream nodes, so this
// stresses per-node view construction (the O(n^2) suspect) and assertion refs.
func genWideDiamond(n int) string {
	var nodes, edges, asserts strings.Builder
	nodes.WriteString(`{"id":"root","type":"request","url":"http://x/root",` +
		`"outputs":[{"name":"val","path":"body.val"}]}`)
	for i := range n {
		fmt.Fprintf(&nodes, `,{"id":"n%d","type":"request","url":"http://x/n%d",`+
			`"outputs":[{"name":"val","path":"body.val"}]}`, i, i)
		fmt.Fprintf(&edges, `{"source":"root","target":"n%d"},`, i)
		fmt.Fprintf(&edges, `{"source":"n%d","target":"sink"},`, i)
		fmt.Fprintf(&asserts, `{"path":"n%d.val","op":"equals","expected":42},`, i)
	}
	fmt.Fprintf(&nodes, `,{"id":"sink","type":"assert","assertions":[%s]}`,
		strings.TrimSuffix(asserts.String(), ","))
	return fmt.Sprintf(`{"name":"wide","nodes":[%s],"edges":[%s]}`,
		nodes.String(), strings.TrimSuffix(edges.String(), ","))
}

// genDeepChain: a chain of N requests, each templating the previous node's output
// into a header and asserting on its own response. Stresses templating + the
// growing output store along a long dependency path.
func genDeepChain(n int) string {
	var nodes, edges strings.Builder
	nodes.WriteString(`{"id":"n0","type":"request","url":"http://x/n0",` +
		`"outputs":[{"name":"val","path":"body.val"}],` +
		`"assertions":[{"path":"status","op":"equals","expected":200}]}`)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&nodes, `,{"id":"n%d","type":"request","url":"http://x/n%d",`+
			`"headers":{"X-Prev":"{{n%d.val}}"},`+
			`"outputs":[{"name":"val","path":"body.val"}],`+
			`"assertions":[{"path":"status","op":"equals","expected":200}]}`, i, i, i-1)
		fmt.Fprintf(&edges, `{"source":"n%d","target":"n%d"},`, i-1, i)
	}
	return fmt.Sprintf(`{"name":"deep","nodes":[%s],"edges":[%s]}`,
		nodes.String(), strings.TrimSuffix(edges.String(), ","))
}

// genLoop: one loop node running an inline request body once per item. Stresses
// sub-flow recursion + per-iteration parse/decode.
func genLoop(items int) string {
	elems := strings.TrimSuffix(strings.Repeat(`1,`, items), ",")
	body := `{"nodes":[{"id":"call","type":"request","url":"http://x/item",` +
		`"outputs":[{"name":"val","path":"body.val"}]}],"edges":[]}`
	return fmt.Sprintf(`{"name":"loop","nodes":[{"id":"lp","type":"loop",`+
		`"items":[%s],"body":%s}],"edges":[]}`, elems, body)
}

// genSSE: consume a stream of k events, asserting on each. Stresses the SSE
// parse + per-event assertion path.
func genSSE(_ int) string {
	return `{"name":"sse","nodes":[{"id":"stream","type":"sse","url":"http://x/sse",` +
		`"max_events":100000,"assertions":[{"path":"n","op":"gte","expected":0}]}],"edges":[]}`
}

// genVarChain: N set_variable nodes, each copying the previous node's value via a
// {{{ref}}} template. Pure engine — no HTTP, no JSON response body — so it
// isolates scheduler + templating + run_when overhead.
func genVarChain(n int) string {
	var nodes, edges strings.Builder
	nodes.WriteString(`{"id":"n0","type":"set_variable","variables":{"v":1}}`)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&nodes, `,{"id":"n%d","type":"set_variable","variables":{"v":"{{{n%d.v}}}"}}`, i, i-1)
		fmt.Fprintf(&edges, `{"source":"n%d","target":"n%d"},`, i-1, i)
	}
	return fmt.Sprintf(`{"name":"vars","nodes":[%s],"edges":[%s]}`,
		nodes.String(), strings.TrimSuffix(edges.String(), ","))
}

// genBranchFanout: a branch with N cases over one root; only the first case is
// taken, so the other N-1 successors cascade-skip. Stresses routing + classify's
// dead-edge / skip-reason path.
func genBranchFanout(n int) string {
	var nodes, edges, cases strings.Builder
	nodes.WriteString(`{"id":"root","type":"set_variable","variables":{"pick":0}}`)
	nodes.WriteString(`,{"id":"br","type":"branch","cases":[`)
	for i := range n {
		fmt.Fprintf(&cases, `{"when":{"path":"root.pick","op":"equals","expected":%d},"target":"c%d"},`, i, i)
	}
	nodes.WriteString(strings.TrimSuffix(cases.String(), ","))
	nodes.WriteString(`]}`)
	edges.WriteString(`{"source":"root","target":"br"},`)
	for i := range n {
		fmt.Fprintf(&nodes, `,{"id":"c%d","type":"set_variable","variables":{"x":%d}}`, i, i)
		fmt.Fprintf(&edges, `{"source":"br","target":"c%d"},`, i)
	}
	return fmt.Sprintf(`{"name":"branch","nodes":[%s],"edges":[%s]}`,
		nodes.String(), strings.TrimSuffix(edges.String(), ","))
}

// genModules: N parallel module nodes, each running the same 3-node child flow.
// Stresses sub-flow recursion + the FlowReferencer validation walk.
func genModules(n int) string {
	var nodes strings.Builder
	nodes.WriteString(`{"id":"m0","type":"module","body_flow_id":"child"}`)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&nodes, `,{"id":"m%d","type":"module","body_flow_id":"child"}`, i)
	}
	return fmt.Sprintf(`{"name":"modules","nodes":[%s],"edges":[]}`, nodes.String())
}

func childFlow() string {
	return `{"name":"child","nodes":[
		{"id":"a","type":"set_variable","variables":{"v":1}},
		{"id":"b","type":"set_variable","variables":{"v":"{{{a.v}}}"}},
		{"id":"c","type":"set_variable","variables":{"v":"{{{b.v}}}"}}],
		"edges":[{"source":"a","target":"b"},{"source":"b","target":"c"}]}`
}

// genPoll: a poll whose body passes on the first attempt (measures the poll
// machinery: body parse + RunInline + per-attempt assertion).
func genPoll() string {
	body := `{"nodes":[{"id":"chk","type":"set_variable","variables":{"ok":1}}],"edges":[]}`
	return fmt.Sprintf(`{"name":"poll","nodes":[{"id":"p","type":"poll","max_attempts":3,`+
		`"body":%s,"assertions":[{"path":"chk.ok","op":"equals","expected":1}]}],"edges":[]}`, body)
}

// genDelayChain: N chained delay nodes (fakeClock — no real sleep), exercising the
// Clock path and a long linear schedule.
func genDelayChain(n int) string {
	var nodes, edges strings.Builder
	nodes.WriteString(`{"id":"n0","type":"delay","duration_ms":1}`)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&nodes, `,{"id":"n%d","type":"delay","duration_ms":1}`, i)
		fmt.Fprintf(&edges, `{"source":"n%d","target":"n%d"},`, i-1, i)
	}
	return fmt.Sprintf(`{"name":"delay","nodes":[%s],"edges":[%s]}`,
		nodes.String(), strings.TrimSuffix(edges.String(), ","))
}

// genComplex: a kitchen-sink graph combining every mechanism — a request root, a
// branch that routes past skipped successors, a foreach loop, a parallel module
// sub-flow, and an assert sink over both. The closest to a real flow.
func genComplex(loopItems int) string {
	elems := strings.TrimSuffix(strings.Repeat(`1,`, loopItems), ",")
	loopBody := `{"nodes":[{"id":"call","type":"request","url":"http://x/item",` +
		`"outputs":[{"name":"val","path":"body.val"}]}],"edges":[]}`
	nodes := fmt.Sprintf(`
		{"id":"root","type":"request","url":"http://x/root","outputs":[{"name":"val","path":"body.val"}]},
		{"id":"br","type":"branch","cases":[
			{"when":{"path":"root.val","op":"equals","expected":42},"target":"lp"},
			{"when":{"path":"root.val","op":"equals","expected":-1},"target":"skipped"}]},
		{"id":"skipped","type":"set_variable","variables":{"x":0}},
		{"id":"lp","type":"loop","items":[%s],"body":%s},
		{"id":"mod","type":"module","body_flow_id":"child"},
		{"id":"sink","type":"assert","assertions":[
			{"path":"lp.count","op":"gte","expected":1},
			{"path":"mod.c.v","op":"equals","expected":1}]}`, elems, loopBody)
	edges := `
		{"source":"root","target":"br"},
		{"source":"br","target":"lp"},{"source":"br","target":"skipped"},
		{"source":"root","target":"mod"},
		{"source":"lp","target":"sink"},{"source":"mod","target":"sink"}`
	return fmt.Sprintf(`{"name":"complex","nodes":[%s],"edges":[%s]}`, nodes, edges)
}

// --- benchmarks -------------------------------------------------------------

func BenchmarkWideDiamond(b *testing.B) {
	for _, n := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			f := mustParse(b, genWideDiamond(n))
			e := engine.New(benchRuntime(instantDoer{}), nil)
			runBench(b, e, f)
		})
	}
}

func BenchmarkDeepChain(b *testing.B) {
	for _, n := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			f := mustParse(b, genDeepChain(n))
			e := engine.New(benchRuntime(instantDoer{}), nil)
			runBench(b, e, f)
		})
	}
}

func BenchmarkLoop(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			f := mustParse(b, genLoop(n))
			e := engine.New(benchRuntime(instantDoer{}), nil)
			runBench(b, e, f)
		})
	}
}

func BenchmarkSSE(b *testing.B) {
	f := mustParse(b, genSSE(0))
	e := engine.New(benchRuntime(instantDoer{sseEvents: 1000}), nil)
	runBench(b, e, f)
}

// BenchmarkRealisticHTTP honors the "wiremock" ask: a real httptest server (real
// sockets, JSON over the loopback) driving a wide-diamond flow. Slower and
// network-dominated — it is an end-to-end sanity check, not an engine profile.
func BenchmarkRealisticHTTP(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedJSON))
	}))
	defer srv.Close()

	j := strings.ReplaceAll(genWideDiamond(32), "http://x", srv.URL)
	f := mustParse(b, j)
	e := engine.New(node.Runtime{HTTP: srv.Client(), Clock: &fakeClock{}}, nil)
	runBench(b, e, f)
}

// childResolve resolves the shared "child" flow used by module benchmarks.
func childResolve(b *testing.B) func(string) (flow.Flow, bool) {
	child := mustParse(b, childFlow())
	return func(id string) (flow.Flow, bool) { return child, id == "child" }
}

func BenchmarkVarChain(b *testing.B) {
	for _, n := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			f := mustParse(b, genVarChain(n))
			runBench(b, engine.New(benchRuntime(instantDoer{}), nil), f)
		})
	}
}

func BenchmarkBranchFanout(b *testing.B) {
	for _, n := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("cases=%d", n), func(b *testing.B) {
			f := mustParse(b, genBranchFanout(n))
			runBench(b, engine.New(benchRuntime(instantDoer{}), nil), f)
		})
	}
}

func BenchmarkModules(b *testing.B) {
	for _, n := range []int{16, 64} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			f := mustParse(b, genModules(n))
			runBench(b, engine.New(benchRuntime(instantDoer{}), childResolve(b)), f)
		})
	}
}

func BenchmarkPoll(b *testing.B) {
	f := mustParse(b, genPoll())
	runBench(b, engine.New(benchRuntime(instantDoer{}), nil), f)
}

func BenchmarkDelayChain(b *testing.B) {
	f := mustParse(b, genDelayChain(64))
	runBench(b, engine.New(benchRuntime(instantDoer{}), nil), f)
}

func BenchmarkComplex(b *testing.B) {
	for _, items := range []int{10, 100} {
		b.Run(fmt.Sprintf("items=%d", items), func(b *testing.B) {
			f := mustParse(b, genComplex(items))
			runBench(b, engine.New(benchRuntime(instantDoer{}), childResolve(b)), f)
		})
	}
}
