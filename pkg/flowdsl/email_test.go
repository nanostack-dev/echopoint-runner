package flowdsl_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl"
)

const signupYAML = `
name: signup
nodes:
  signup:
    request:
      method: post
      url: "{{apiUrl}}/signup"
      body:
        email: "{{inbox.local}}+alice@{{inbox.domain}}"
    expect:
      status: 202
    next: verify
  verify:
    email_wait:
      timeout_ms: 120000
      link_pattern: ^https://staging\.acme\.example/verify
      code_pattern: 'code is (\d{6})'
    expect:
      - $.subject contains Verify
      - $.envelope_to contains +alice@
      - html:a.verify@href starts_with https://
      - '"regex:code is \\d+" not_empty'
    outputs:
      code:
        path: $.code
        secret: true
      button: html:a.verify@href
      title:
        html: h1
      every:
        html: a
        attribute: href
        all: true
      ref:
        regex: 'Ref (\w+)'
        group: 1
    next: confirm
  confirm:
    request:
      method: get
      url: "{{verify.first_link}}?code={{verify.code}}&b={{verify.button}}"
`

func compileOne(t *testing.T, src string) flowdsl.Outcome {
	t.Helper()
	return flowdsl.CompileSource("signup", []byte(src))
}

func TestEmailWaitCompilesToTheRunnerNode(t *testing.T) {
	out := compileOne(t, signupYAML)
	if errs := out.Errors(); len(errs) > 0 {
		t.Fatalf("compile: %s", joinDiags(errs))
	}
	raw, err := json.Marshal(out.Flows[0].Request.FlowDefinition)
	if err != nil {
		t.Fatal(err)
	}
	var def struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatal(err)
	}
	var verify map[string]any
	for _, n := range def.Nodes {
		if n["id"] == "verify" {
			verify = n
		}
	}
	if verify == nil || verify["type"] != "email_wait" {
		t.Fatalf("verify node: %v", verify)
	}
	data, _ := verify["data"].(map[string]any)
	if data["timeout_ms"] != float64(120000) || data["code_pattern"] != `code is (\d{6})` ||
		data["link_pattern"] != `^https://staging\.acme\.example/verify` {
		t.Errorf("data: %v", data)
	}
	sorted, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	text := string(sorted)
	for _, want := range []string{
		`"extractor_data":{"attribute":"href","selector":"a.verify"},"extractor_type":"html"`,
		`"extractor_data":{"pattern":"code is \\d+"},"extractor_type":"regex"`,
		`"extractor":{"attribute":"href","selector":"a.verify","type":"html"}`,
		`"extractor":{"all":true,"attribute":"href","selector":"a","type":"html"}`,
		`"extractor":{"group":1,"pattern":"Ref (\\w+)","type":"regex"}`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("compiled flow lacks %s\n%s", want, text)
		}
	}
}

func TestEmailWaitRoundTrips(t *testing.T) {
	out := compileOne(t, signupYAML)
	if errs := out.Errors(); len(errs) > 0 {
		t.Fatalf("compile: %s", joinDiags(errs))
	}
	raw, err := json.Marshal(out.Flows[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	assertRoundTrip(t, storedJSON(t, raw))
}

func TestEmailWaitExpectGroupsRoundTrip(t *testing.T) {
	out := compileOne(t, `
name: once
nodes:
  verify:
    email_wait:
      settle_ms: 2000
      expect:
        - name: verification
          max: 1
          checks:
            - $.subject contains Verify
`)
	if errs := out.Errors(); len(errs) > 0 {
		t.Fatalf("compile: %s", joinDiags(errs))
	}
	raw, err := json.Marshal(out.Flows[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	assertRoundTrip(t, storedJSON(t, raw))
}

func TestEmailWaitLint(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"timeout past the address lifetime": {`
name: x
nodes:
  verify:
    email_wait:
      timeout_ms: 7200001
`, "must not exceed 7200000"},
		"bad code pattern": {`
name: x
nodes:
  verify:
    email_wait:
      code_pattern: "("
`, "code_pattern must be a regular expression"},
		"undeclared output": {`
name: x
nodes:
  verify:
    email_wait: {}
    next: use
  use:
    request:
      method: get
      url: "{{verify.magic}}"
`, `does not declare output "magic"`},
		"expect mode publishes only received": {`
name: x
nodes:
  verify:
    email_wait:
      expect:
        - name: a
          checks: [$.subject contains A]
    next: use
  use:
    request:
      method: get
      url: "{{verify.code}}"
`, `does not declare output "code"`},
		"unknown inbox input": {`
name: x
nodes:
  use:
    request:
      method: get
      url: "{{inbox.password}}"
`, `node "inbox" does not exist`},
	} {
		t.Run(name, func(t *testing.T) {
			errs := compileOne(t, tc.src).Errors()
			if !strings.Contains(joinDiags(errs), tc.want) {
				t.Fatalf("want %q, got %s", tc.want, joinDiags(errs))
			}
		})
	}
}

// storedJSON turns a CreateFlowRequest into the stored flow shape the round
// trip reads: an id and a folder, as the API returns it.
func storedJSON(t *testing.T, request []byte) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(request, &doc); err != nil {
		t.Fatal(err)
	}
	doc["id"] = "44444444-4444-4444-8444-444444444444"
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
