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
	out := compileSource(t, "name: cleared\ndescription:\n"+oneNodeFlow)
	if len(out.Errors()) != 0 {
		t.Fatal(joinDiags(out.Errors()))
	}
	if got := out.Flows[0].Request.Description; got == nil || *got != "" {
		t.Fatalf("description: %v", got)
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
