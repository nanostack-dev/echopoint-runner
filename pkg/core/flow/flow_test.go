package flow_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
)

func TestParse(t *testing.T) {
	f, err := flow.Parse([]byte(`{"name":"f","inputs":{"a":1},
		"nodes":[{"id":"x","type":"delay","duration_ms":5}],"edges":[]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Name != "f" || len(f.Nodes) != 1 || f.Nodes[0].ID != "x" || f.Nodes[0].Kind != "delay" {
		t.Fatalf("parsed wrong: %+v", f)
	}
	if v, ok := f.Inputs["a"].Int(); !ok || v != 1 {
		t.Fatalf("inputs not parsed: %v", f.Inputs)
	}
}

func TestParseInvalidJSON(t *testing.T) {
	if _, err := flow.Parse([]byte(`{not json`)); err == nil {
		t.Fatal("invalid JSON should error")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name, json string
		wantErr    bool
	}{
		{
			"ok",
			`{"nodes":[{"id":"a","type":"delay"},{"id":"b","type":"delay"}],"edges":[{"source":"a","target":"b"}]}`,
			false,
		},
		{"dup id", `{"nodes":[{"id":"a","type":"delay"},{"id":"a","type":"delay"}],"edges":[]}`, true},
		{"empty id", `{"nodes":[{"id":"","type":"delay"}],"edges":[]}`, true},
		{"edge to unknown", `{"nodes":[{"id":"a","type":"delay"}],"edges":[{"source":"a","target":"ghost"}]}`, true},
		{"edge from unknown", `{"nodes":[{"id":"a","type":"delay"}],"edges":[{"source":"ghost","target":"a"}]}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := flow.Parse([]byte(c.json))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if gotErr := flow.Validate(f) != nil; gotErr != c.wantErr {
				t.Fatalf("Validate err=%v, want err=%v", gotErr, c.wantErr)
			}
		})
	}
}
