package flowdsl_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl"
	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl/wire"
)

func TestCompileAndDecompileContractNodes(t *testing.T) {
	t.Parallel()

	source := `name: Provision user
role: module
nodes:
  in:
    input:
      required: [email]
      properties:
        email:
          type: string
          format: email
          description: User email address
        plan:
          type: string
          enum: [starter, pro]
          default: pro
        api_secret:
          type: string
          secret: true
    next: out
  out:
    output:
      properties:
        summary:
          type: string
          description: Formatted user summary
          value: "{{in.email}}:{{in.plan}}"
        token:
          type: string
          value: "{{in.api_secret}}"
          secret: true
`
	outcome := flowdsl.CompileSource("provision.yaml", []byte(source))
	if errs := outcome.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected compile errors: %v", errs)
	}
	if len(outcome.Flows) != 1 {
		t.Fatalf("compiled %d flows, want 1", len(outcome.Flows))
	}

	req := outcome.Flows[0].Request
	if len(req.FlowDefinition.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(req.FlowDefinition.Nodes))
	}

	decompiled, err := flowdsl.Decompile(wire.Flow{
		Name:           req.Name,
		Role:           *req.Role,
		Version:        *req.Version,
		FlowDefinition: req.FlowDefinition,
	})
	if err != nil {
		t.Fatalf("decompile: %v", err)
	}

	roundTrip := flowdsl.CompileSource("provision-rt.yaml", decompiled)
	if errs := roundTrip.Errors(); len(errs) > 0 {
		t.Fatalf("round-trip compile errors: %v\nyaml:\n%s", errs, string(decompiled))
	}
	wantJSON, err := json.Marshal(req.FlowDefinition)
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}
	gotJSON, err := json.Marshal(roundTrip.Flows[0].Request.FlowDefinition)
	if err != nil {
		t.Fatalf("marshal round-trip: %v", err)
	}
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("round-trip mismatch:\nwant: %s\ngot:  %s", wantJSON, gotJSON)
	}
}

func TestContractNodeDiagnostics(t *testing.T) {
	t.Parallel()

	t.Run("missing output value", func(t *testing.T) {
		t.Parallel()
		source := `name: Bad output
role: module
nodes:
  out:
    output:
      properties:
        summary:
          type: string
`
		outcome := flowdsl.CompileSource("bad.yaml", []byte(source))
		errs := outcome.Errors()
		if len(errs) == 0 || !strings.Contains(errs[0].Message, "needs a value") {
			t.Fatalf("expected missing value error, got %v", errs)
		}
	})

	t.Run("input inside loop body", func(t *testing.T) {
		t.Parallel()
		source := `name: Loop smuggle
nodes:
  each:
    loop:
      items: [a]
      body:
        nodes:
          in:
            input:
              properties:
                x:
                  type: string
`
		outcome := flowdsl.CompileSource("loop.yaml", []byte(source))
		errs := outcome.Errors()
		if len(errs) == 0 || !strings.Contains(errs[0].Message, "not allowed inside a loop or poll body") {
			t.Fatalf("expected inline body rejection, got %v", errs)
		}
	})
}
