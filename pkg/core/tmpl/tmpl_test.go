package tmpl_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/tmpl"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

func TestResolve(t *testing.T) {
	view := value.Of(map[string]any{
		"name":  "alice", // flow input (bare)
		"login": map[string]any{"token": "xyz", "obj": map[string]any{"a": float64(1)}},
	})
	dyn := func(name string, args []string) (string, error) {
		if name == "up" && len(args) > 0 {
			return "U:" + args[0], nil
		}
		return "", errors.New("unknown")
	}

	got, err := tmpl.Resolve(json.RawMessage(`{
		"greet": "hi {{name}}",
		"auth":  "Bearer {{login.token}}",
		"raw":   "{{{login.obj}}}",
		"dyn":   "{{$up:x}}",
		"miss":  "{{nope.x}}"
	}`), view, dyn)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if uerr := json.Unmarshal(got, &out); uerr != nil {
		t.Fatal(uerr)
	}
	if out["greet"] != "hi alice" {
		t.Fatalf("greet=%v", out["greet"])
	}
	if out["auth"] != "Bearer xyz" {
		t.Fatalf("auth=%v", out["auth"])
	}
	// {{{raw}}} preserves structure (object, not a string)
	if m, ok := out["raw"].(map[string]any); !ok || m["a"] != float64(1) {
		t.Fatalf("raw=%v (should be an object)", out["raw"])
	}
	if out["dyn"] != "U:x" {
		t.Fatalf("dyn=%v", out["dyn"])
	}
	// unresolved refs are left verbatim (typo is visible)
	if out["miss"] != "{{nope.x}}" {
		t.Fatalf("miss=%v (should be literal)", out["miss"])
	}
}

func TestResolveNilDynLeavesLiteral(t *testing.T) {
	got, err := tmpl.Resolve(json.RawMessage(`{"x":"{{$uuid}}"}`), value.Value{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(got, &out)
	if out["x"] != "{{$uuid}}" {
		t.Fatalf("nil resolver should leave {{$uuid}} literal, got %v", out["x"])
	}
}

// TestResolveByteScannerEdgeCases locks the byte-level rewriter to the
// semantics of the unmarshal/walk/remarshal it replaced.
func TestResolveByteScannerEdgeCases(t *testing.T) {
	view := value.Of(map[string]any{
		"q":   `he said "hi"`,
		"n":   float64(5),
		"obj": map[string]any{"a": float64(1)},
	})

	got, err := tmpl.Resolve(json.RawMessage(`{
		"{{n}}":  "keys are never templated",
		"esc":    "prefix \"quoted\" {{q}} suffix",
		"uni":    "\u007b\u007bn}}",
		"static": "no tokens here",
		"arr":    ["{{{n}}}", "x {{n}} y", "{{{obj}}}"],
		"num":    12.5
	}`), view, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if uerr := json.Unmarshal(got, &out); uerr != nil {
		t.Fatalf("resolved output is not valid JSON: %v\n%s", uerr, got)
	}
	if _, ok := out["{{n}}"]; !ok {
		t.Fatalf("template key must stay verbatim, got keys %v", out)
	}
	if out["esc"] != `prefix "quoted" he said "hi" suffix` {
		t.Fatalf("esc=%q", out["esc"])
	}
	// {{ decodes to "{{" — the escaped form is still a template.
	if out["uni"] != "5" {
		t.Fatalf("uni=%q (unicode-escaped token must resolve)", out["uni"])
	}
	if out["static"] != "no tokens here" {
		t.Fatalf("static=%q", out["static"])
	}
	arr, _ := out["arr"].([]any)
	if len(arr) != 3 || arr[0] != float64(5) || arr[1] != "x 5 y" {
		t.Fatalf("arr=%v", arr)
	}
	if m, ok := arr[2].(map[string]any); !ok || m["a"] != float64(1) {
		t.Fatalf("arr[2]=%v (structural substitution in array)", arr[2])
	}
	if out["num"] != 12.5 {
		t.Fatalf("num=%v", out["num"])
	}
}
