package value_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

func TestAccessors(t *testing.T) {
	v := value.JSON([]byte(`{"n":7,"s":"hi","b":true,"arr":[1,2,3],"nested":{"x":{"y":5}}}`))

	nv, ok := v.Get("n")
	if i, iok := nv.Int(); !ok || !iok || i != 7 {
		t.Fatalf("n: %v %v", i, iok)
	}
	sv, _ := v.Get("s")
	if s, _ := sv.Str(); s != "hi" {
		t.Fatalf("s=%q", s)
	}
	bv, _ := v.Get("b")
	if b, _ := bv.Bool(); !b {
		t.Fatal("b")
	}
	// bare path and explicit jsonpath both reach nested values
	for _, path := range []string{"nested.x.y", "$.nested.x.y"} {
		yv, yok := v.Get(path)
		if y, _ := yv.Int(); !yok || y != 5 {
			t.Fatalf("%s: %v %v", path, y, yok)
		}
	}
	// wildcard returns a list
	av, aok := v.Get("$.arr[*]")
	if l, lok := av.List(); !aok || !lok || len(l) != 3 {
		t.Fatalf("arr[*]: %v", l)
	}
	if _, found := v.Get("nope"); found {
		t.Fatal("missing path should report false")
	}
}

func TestZeroAndBoxing(t *testing.T) {
	if !(value.Value{}).IsZero() {
		t.Fatal("zero Value should be zero")
	}
	if value.Of(1).IsZero() {
		t.Fatal("Of(1) should not be zero")
	}
	if !value.JSON(nil).IsZero() {
		t.Fatal("empty JSON should be zero")
	}
	m := value.Map{"a": value.Of(1), "b": value.Of("x")}
	av, _ := m.Value().Get("a")
	if i, _ := av.Int(); i != 1 {
		t.Fatalf("Map.Value a=%v", i)
	}
}

func TestMarshal(t *testing.T) {
	b, err := json.Marshal(value.Of(map[string]any{"k": "v"}))
	if err != nil || string(b) != `{"k":"v"}` {
		t.Fatalf("marshal: %s (%v)", b, err)
	}
}

// TestGetDottedEdgeCases locks the dotted fast path to the semantics of the
// bracket-quoted JSONPath it replaced: exact member-name lookup per segment.
func TestGetDottedEdgeCases(t *testing.T) {
	v := value.Of(map[string]any{
		"content-type": "json",
		"a":            map[string]any{"": map[string]any{"b": float64(7)}},
		"null":         nil,
		"arr":          []any{"x"},
	})
	if got, ok := v.Get("content-type"); !ok || got.Raw() != "json" {
		t.Fatalf("hyphenated member: %v %v", got, ok)
	}
	if got, ok := v.Get("a..b"); !ok { // empty segment = empty member name
		t.Fatalf("empty segment: %v %v", got, ok)
	} else if i, _ := got.Int(); i != 7 {
		t.Fatalf("a..b = %v", got)
	}
	if got, ok := v.Get("null"); !ok || got.Raw() != nil {
		t.Fatalf("null member should be found: %v %v", got, ok)
	}
	if _, ok := v.Get("arr.0"); ok {
		t.Fatal("name lookup into an array must not match (same as ['0'])")
	}
	if _, ok := v.Get("a.missing"); ok {
		t.Fatal("missing nested member should report false")
	}
	if got, ok := v.Get(""); !ok || got.Raw() == nil {
		t.Fatalf("empty path selects the root: %v %v", got, ok)
	}
	if got, ok := v.Get("$['content-type']"); !ok || got.Raw() != "json" {
		t.Fatalf("full JSONPath still works: %v %v", got, ok)
	}
}

// TestValueUnmarshal locks the single-pass decode of value.Value / value.Map.
func TestValueUnmarshal(t *testing.T) {
	var m value.Map
	if err := json.Unmarshal([]byte(`{"n":1,"s":"x","o":{"k":true},"z":null}`), &m); err != nil {
		t.Fatal(err)
	}
	if i, _ := m["n"].Int(); i != 1 {
		t.Fatalf("n=%v", m["n"])
	}
	if s, _ := m["s"].Str(); s != "x" {
		t.Fatalf("s=%v", m["s"])
	}
	if got, ok := m["o"].Get("k"); !ok || got.Raw() != true {
		t.Fatalf("o.k=%v %v", got, ok)
	}
	if !m["z"].IsZero() {
		t.Fatal("null should decode to the zero Value")
	}
}
