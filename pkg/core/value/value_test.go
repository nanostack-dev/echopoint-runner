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
