package flowdsl_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl"
)

const oneNodeFlow = `
nodes:
  call:
    request:
      method: GET
      url: "{{base}}/x"
`

func compileSource(t *testing.T, src string) flowdsl.Outcome {
	t.Helper()
	return flowdsl.CompileSource("flow", []byte(strings.TrimSpace(src)+"\n"))
}

func TestExplicitEmptyDescriptionAndTagsLowerToClears(t *testing.T) {
	out := compileSource(t, "name: cleared\ndescription: \"\"\ntags: []\n"+oneNodeFlow)
	if len(out.Errors()) != 0 {
		t.Fatal(joinDiags(out.Errors()))
	}
	req := out.Flows[0].Request
	if req.Description == nil || *req.Description != "" {
		t.Fatalf("description: %v", req.Description)
	}
	if req.FlowDefinition.Description == nil || *req.FlowDefinition.Description != "" {
		t.Fatalf("definition description: %v", req.FlowDefinition.Description)
	}
	if req.Tags == nil || len(*req.Tags) != 0 {
		t.Fatalf("tags: %v", req.Tags)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"description":""`) || !strings.Contains(string(raw), `"tags":[]`) {
		t.Fatalf("lowered JSON: %s", raw)
	}
}

func TestNullDescriptionLowersToAClear(t *testing.T) {
	for _, value := range []string{"", " ~", " null"} {
		out := compileSource(t, "name: cleared\ndescription:"+value+"\n"+oneNodeFlow)
		if len(out.Errors()) != 0 {
			t.Fatal(joinDiags(out.Errors()))
		}
		if got := out.Flows[0].Request.Description; got == nil || *got != "" {
			t.Fatalf("description:%s lowered to %v", value, got)
		}
	}
}

func TestAbsentDescriptionAndTagsStayUnset(t *testing.T) {
	out := compileSource(t, "name: plain\n"+oneNodeFlow)
	if len(out.Errors()) != 0 {
		t.Fatal(joinDiags(out.Errors()))
	}
	req := out.Flows[0].Request
	if req.Description != nil || req.FlowDefinition.Description != nil || req.Tags != nil {
		t.Fatalf("description %v, definition %v, tags %v", req.Description, req.FlowDefinition.Description, req.Tags)
	}
}

func assertOneError(t *testing.T, out flowdsl.Outcome, want string) {
	t.Helper()
	errs := out.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Message, want) {
		t.Fatalf("want one error with %q, got: %s", want, joinDiags(errs))
	}
	if len(out.Flows) != 0 {
		t.Fatalf("a flow with an error compiled: %d flows", len(out.Flows))
	}
}

func TestNextBackEdgeIsACycleError(t *testing.T) {
	out := compileSource(t, `
name: loop back
nodes:
  a:
    set:
      x: 1
    next: b
  b:
    set:
      y: 2
    next: a
`)
	assertOneError(t, out, `node "b" next "a" makes a cycle: a -> b -> a`)
}

func TestSelfNextIsACycleError(t *testing.T) {
	out := compileSource(t, `
name: self
nodes:
  a:
    set:
      x: 1
    next: a
`)
	assertOneError(t, out, `node "a" next "a" makes a cycle: a -> a`)
}

func TestBranchDefaultBackToAnAncestorIsACycleError(t *testing.T) {
	out := compileSource(t, `
name: retry by branch
nodes:
  call:
    request:
      method: GET
      url: "{{base}}/x"
    outputs:
      state: $.state
    next: route
  route:
    branch:
      target: "{{call.state}}"
      cases:
        - when: "body equals ready"
          target: done
      default: call
  done:
    set:
      ok: true
`)
	assertOneError(t, out, `node "route" default "call" makes a cycle: call -> route -> call`)
}

func TestFailureBackEdgeIsACycleError(t *testing.T) {
	out := compileSource(t, `
name: fail back
nodes:
  a:
    set:
      x: 1
    next: b
  b:
    run_when: always
    set:
      y: 2
    on_failure: a
`)
	assertOneError(t, out, `node "b" on_failure "a" makes a cycle: a -> b -> a`)
}

func TestCycleInsideAPollBodyIsAnError(t *testing.T) {
	out := compileSource(t, `
name: poll cycle
nodes:
  wait:
    poll:
      max_attempts: 3
      interval_ms: 100
      body:
        nodes:
          probe:
            set:
              x: 1
            next: again
          again:
            set:
              y: 2
            next: probe
`)
	assertOneError(t, out, `node "again" next "probe" makes a cycle: probe -> again -> probe`)
}

func TestDiamondIsNotACycle(t *testing.T) {
	out := compileSource(t, `
name: diamond
nodes:
  a:
    set:
      x: 1
    next: [b, c]
  b:
    set:
      y: 2
    next: d
  c:
    set:
      z: 3
    next: d
  d:
    set:
      w: 4
`)
	if len(out.Errors()) != 0 {
		t.Fatal(joinDiags(out.Errors()))
	}
}

func TestASecondYAMLDocumentIsAnError(t *testing.T) {
	out := compileSource(t, "name: first\n"+strings.TrimSpace(oneNodeFlow)+"\n---\nname: second\n"+oneNodeFlow)
	assertOneError(t, out, "more than one YAML document")
	if line := out.Errors()[0].Line; line != 8 {
		t.Fatalf("error line %d, want 8 (the second document)", line)
	}
}

func TestABrokenSecondYAMLDocumentIsAnError(t *testing.T) {
	out := compileSource(t, "name: first\n"+strings.TrimSpace(oneNodeFlow)+"\n---\nname: [\n")
	assertOneError(t, out, "YAML:")
}

func TestATrailingDocumentMarkerStillCompiles(t *testing.T) {
	for _, tail := range []string{"---\n", "---\n...\n", "--- # end\n"} {
		src := "name: one\n" + strings.TrimSpace(oneNodeFlow) + "\n" + tail
		out := flowdsl.CompileSource("flow", []byte(src))
		if len(out.Errors()) != 0 || len(out.Flows) != 1 {
			t.Fatalf("%q: %s", tail, joinDiags(out.Errors()))
		}
	}
}

func TestAnEmptyFileIsStillReportedAsEmpty(t *testing.T) {
	for _, src := range []string{"", "# only a comment\n"} {
		out := flowdsl.CompileSource("flow", []byte(src))
		if errs := out.Errors(); len(errs) != 1 || errs[0].Message != "file is empty" || errs[0].Line != 1 {
			t.Fatalf("%q: %s", src, joinDiags(errs))
		}
	}
}
