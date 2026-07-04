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

func TestSchemaVersion(t *testing.T) {
	// unstamped → defaults to the current schema
	f, _ := flow.Parse([]byte(`{"nodes":[{"id":"a","type":"delay"}],"edges":[]}`))
	if f.SchemaVersion != flow.CurrentSchemaVersion {
		t.Fatalf("unstamped flow should default to current, got %d", f.SchemaVersion)
	}
	if err := flow.Validate(f); err != nil {
		t.Fatalf("current-version flow should validate: %v", err)
	}

	// explicit current version is accepted
	cur, _ := flow.Parse([]byte(`{"schema_version":1,"nodes":[{"id":"a","type":"delay"}],"edges":[]}`))
	if cur.SchemaVersion != 1 || flow.Validate(cur) != nil {
		t.Fatalf("explicit v1 should validate, got v%d", cur.SchemaVersion)
	}

	// a newer version than the runner speaks is rejected loudly
	future, _ := flow.Parse([]byte(`{"schema_version":999,"nodes":[{"id":"a","type":"delay"}],"edges":[]}`))
	if err := flow.Validate(future); err == nil {
		t.Fatal("a newer schema version must be rejected")
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
