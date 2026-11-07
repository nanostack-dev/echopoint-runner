package flowdsl_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl"
)

const stripeYAML = `
name: "stripe: invoice paid"
nodes:
  customer:
    integration:
      id: stripe
      operation: customer.create
      inputs:
        email: garbage+stripe@nanostack.dev
        name: "{{customerName}}"
      timeout_ms: 10000
    outputs:
      customerId: $.id
    expect:
      status: 200
    next: lookup
  lookup:
    integration:
      id: stripe
      operation: customer.retrieve
      inputs:
        customer: "{{customer.id}}"
    next: paid
  paid:
    integration:
      id: stripe
      event: customer.created
      timeout_ms: 60000
    expect: '$.data.object.id equals {{customer.customerId}}'
    outputs:
      eventId: $.body.id
    next: done
  done:
    assert: {target: "{{paid.body}}"}
    expect: "$.type equals customer.created"
`

func TestIntegrationNodesCompileToRequestAndWebhookWait(t *testing.T) {
	out := flowdsl.CompileSource("stripe", []byte(stripeYAML))
	if len(out.Errors()) > 0 {
		t.Fatal(joinDiags(out.Errors()))
	}
	req := out.Flows[0].Request

	customer := nodeByID(t, req, "customer")
	if customer["type"] != "request" {
		t.Fatalf("customer type: %v", customer["type"])
	}
	data := object(t, customer, "data")
	integ := object(t, data, "integration")
	if integ["id"] != "stripe" || integ["operation"] != "customer.create" || integ["event"] != nil {
		t.Fatalf("customer integration: %#v", integ)
	}
	inputs := object(t, integ, "inputs")
	if inputs["email"] != "garbage+stripe@nanostack.dev" || inputs["name"] != "{{customerName}}" {
		t.Fatalf("customer inputs: %#v", inputs)
	}
	if data["timeout"] != float64(10000) || data["method"] != "" || data["url"] != "" {
		t.Fatalf("customer data: %#v", data)
	}
	for _, key := range []string{"headers", "body", "query_params"} {
		if _, set := data[key]; set {
			t.Fatalf("customer data sets %s: %#v", key, data)
		}
	}
	assertStatus(t, customer, "200")

	paid := nodeByID(t, req, "paid")
	if paid["type"] != "webhook_wait" {
		t.Fatalf("paid type: %v", paid["type"])
	}
	data = object(t, paid, "data")
	integ = object(t, data, "integration")
	if integ["id"] != "stripe" || integ["event"] != "customer.created" || integ["operation"] != nil ||
		integ["inputs"] != nil {
		t.Fatalf("paid integration: %#v", integ)
	}
	if data["timeout_ms"] != float64(60000) {
		t.Fatalf("paid data: %#v", data)
	}
	assertCheck(t, paid, "$.data.object.id", "equals", "{{customer.customerId}}")
}

func TestIntegrationNodeRejections(t *testing.T) {
	cases := []struct {
		name string
		node string
		want string
	}{
		{
			name: "http fields",
			node: "integration: {id: stripe, operation: customer.list, method: GET, url: https://x}",
			want: "remove method, url, body and headers",
		},
		{
			name: "both",
			node: "integration: {id: stripe, operation: customer.list, event: invoice.paid}",
			want: "set exactly one of integration.operation and integration.event",
		},
		{
			name: "neither",
			node: "integration: {id: stripe}",
			want: "set exactly one of integration.operation and integration.event",
		},
		{
			name: "no id",
			node: "integration: {operation: customer.list}",
			want: "integration.id is required",
		},
		{
			name: "event inputs",
			node: "integration: {id: stripe, event: invoice.paid, inputs: {limit: 1}}",
			want: "an event takes no inputs",
		},
		{
			name: "event status",
			node: "integration: {id: stripe, event: invoice.paid}\n    expect:\n      status: 200",
			want: "assert #1: an event has no status",
		},
		{
			name: "operation settle",
			node: "integration: {id: stripe, operation: customer.list, settle_ms: 10}",
			want: `unknown field "settle_ms"`,
		},
		{
			name: "two kinds",
			node: "integration: {id: stripe, operation: customer.list}\n    request: {method: GET, url: x}",
			want: "has more than one kind",
		},
		{
			name: "node inputs",
			node: "integration: {id: stripe, operation: customer.list}\n    inputs: {limit: 1}",
			want: "inputs are only for a module",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf("name: bad\nnodes:\n  call:\n    %s\n", tc.node)
			out := flowdsl.CompileSource("bad", []byte(src))
			if !strings.Contains(joinDiags(out.Errors()), tc.want) {
				t.Fatalf("want %q in errors: %s", tc.want, joinDiags(out.Errors()))
			}
		})
	}
}

func TestIntegrationEventOutputsAreChecked(t *testing.T) {
	src := `
name: refs
nodes:
  paid:
    integration: {id: stripe, event: invoice.paid}
    next: use
  use:
    request:
      method: GET
      url: "{{base}}/{{paid.body.id}}/{{paid.nope}}"
`
	out := flowdsl.CompileSource("refs", []byte(src))
	diags := joinDiags(out.Errors())
	if len(out.Errors()) != 1 || !strings.Contains(diags, `does not declare output "nope"`) {
		t.Fatalf("errors: %s", diags)
	}
}

func TestIntegrationInputsAreLinted(t *testing.T) {
	src := `
name: refs
nodes:
  call:
    integration:
      id: stripe
      operation: customer.retrieve
      inputs:
        customer: "{{missing.id}}"
`
	out := flowdsl.CompileSource("refs", []byte(src))
	if !strings.Contains(joinDiags(out.Errors()), `node "missing" does not exist`) {
		t.Fatalf("errors: %s", joinDiags(out.Errors()))
	}
}

func TestFillIntegrationsSetsTheOperationTarget(t *testing.T) {
	src := stripeYAML + `
  again:
    poll:
      max_attempts: 2
      body:
        nodes:
          inner:
            integration: {id: stripe, operation: customer.list}
`
	out := flowdsl.CompileSource("stripe", []byte(src))
	if len(out.Errors()) > 0 {
		t.Fatal(joinDiags(out.Errors()))
	}
	req := out.Flows[0].Request
	seen := map[string]bool{}
	err := flowdsl.FillIntegrations(&req, func(node flowdsl.IntegrationNode) (flowdsl.Target, error) {
		seen[node.NodeID] = node.IsEvent()
		switch {
		case node.IsEvent():
			return flowdsl.Target{}, nil
		case *node.Ref.Operation == "customer.retrieve":
			return flowdsl.Target{}, errors.New("customer.retrieve is not offered")
		default:
			return flowdsl.Target{Method: "post", URL: "/v1/customers"}, nil
		}
	})
	if err == nil || !strings.Contains(err.Error(), "customer.retrieve is not offered") {
		t.Fatalf("err: %v", err)
	}
	want := map[string]bool{"customer": false, "lookup": false, "paid": true, "again/inner": false}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("seen %v, want %v", seen, want)
	}
	data := object(t, nodeByID(t, req, "customer"), "data")
	if data["method"] != "POST" || data["url"] != "/v1/customers" {
		t.Fatalf("customer data: %#v", data)
	}
	poll := object(t, nodeByID(t, req, "again"), "data")
	inner := findByID(t, array(t, object(t, poll, "body"), "nodes"), "inner")
	if object(t, inner, "data")["method"] != "POST" {
		t.Fatalf("inner: %#v", inner)
	}
	if !json.Valid(mustJSON(t, req)) {
		t.Fatal("filled request is not JSON")
	}
}

// integrationJSON is a stored flow the MCP build_flow tool writes: the
// operation node carries the catalog's method and url.
const integrationJSON = `{
  "id": "44444444-4444-4444-8444-444444444444",
  "name": "integration shapes",
  "role": "standalone",
  "version": "1.0",
  "flow_definition": {
    "name": "integration shapes",
    "version": "1.0",
    "nodes": [
      {
        "id": "customer", "type": "request", "display_name": "customer", "run_when": "on_success",
        "data": {
          "method": "POST", "url": "/v1/customers", "timeout": 5000,
          "integration": {"id": "stripe", "operation": "customer.create", "inputs": {"email": "garbage+stripe@nanostack.dev"}}
        },
        "assertions": [{"extractor_type": "status_code", "extractor_data": {}, "operator_type": "equals", "operator_data": {"value": "200"}}]
      },
      {
        "id": "created", "type": "webhook_wait", "display_name": "created", "run_when": "on_success",
        "data": {
          "timeout_ms": 60000, "settle_ms": 1000,
          "integration": {"id": "stripe", "event": "customer.created"},
          "expect": [{"name": "created", "assertions": []}]
        },
        "outputs": [{"name": "eventId", "extractor": {"type": "json_path", "path": "$.body.id"}}]
      }
    ],
    "edges": [{"id": "customer-created", "source": "customer", "target": "created", "type": "success"}]
  }
}`

func TestDecompileRoundTripsIntegrationNodes(t *testing.T) {
	flow, err := storedFlow([]byte(integrationJSON))
	if err != nil {
		t.Fatal(err)
	}
	file, err := flowdsl.Decompile(flow)
	if err != nil {
		t.Fatal(err)
	}
	text := string(file)
	for _, want := range []string{"integration:", "operation: customer.create", "event: customer.created"} {
		if !strings.Contains(text, want) {
			t.Fatalf("file lacks %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"method:", "url:", "request:", "webhook_wait:"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("file has %q:\n%s", unwanted, text)
		}
	}
	assertRoundTrip(t, []byte(integrationJSON))
}

func TestDecompiledIntegrationFileCompilesAndFills(t *testing.T) {
	flow, err := storedFlow([]byte(integrationJSON))
	if err != nil {
		t.Fatal(err)
	}
	file, err := flowdsl.Decompile(flow)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "stripe.flow.yaml")
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := flowdsl.CompileFile(path, dir)
	if err != nil || len(out.Errors()) > 0 {
		t.Fatalf("compile: %v %s", err, joinDiags(out.Errors()))
	}
	req := out.Flows[0].Request
	err = flowdsl.FillIntegrations(&req, func(flowdsl.IntegrationNode) (flowdsl.Target, error) {
		return flowdsl.Target{Method: "POST", URL: "/v1/customers"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if problems := diffFlows([]byte(integrationJSON), mustJSON(t, req)); len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAnIntegrationEventWithoutARunCheckWarns(t *testing.T) {
	cases := map[string]struct {
		node string
		warn bool
	}{
		"no check": {node: `
  paid:
    integration: {id: stripe, event: customer.created}`, warn: true},
		"literal check": {node: `
  paid:
    integration: {id: stripe, event: customer.created}
    expect: '$.data.object.email equals garbage+stripe@nanostack.dev'`, warn: true},
		"node expect on a created value": {node: `
  paid:
    integration: {id: stripe, event: customer.created}
    expect: '$.data.object.id equals {{customer.id}}'`},
		"group check on a created value": {node: `
  paid:
    integration:
      id: stripe
      event: customer.created
      expect:
        - name: created
          checks: ['$.data.object.id equals {{customer.id}}']`},
		"flow input": {node: `
  paid:
    integration: {id: stripe, event: customer.created}
    expect: '$.data.object.id equals {{customerId}}'`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := `
name: correlate
inputs: [customerId]
nodes:
  customer:
    integration:
      id: stripe
      operation: customer.create
      inputs: {email: garbage+stripe@nanostack.dev}
    next: paid` + tc.node + "\n"
			out := flowdsl.CompileSource("correlate", []byte(src))
			if len(out.Errors()) > 0 {
				t.Fatal(joinDiags(out.Errors()))
			}
			warned := false
			for _, d := range out.Warnings() {
				if strings.Contains(d.Message, "no check tied to this run") {
					warned = true
				}
			}
			if warned != tc.warn {
				t.Fatalf("warned=%v, want %v: %v", warned, tc.warn, out.Warnings())
			}
		})
	}
}
