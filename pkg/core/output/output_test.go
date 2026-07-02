package output_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/output"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

func TestExtract(t *testing.T) {
	v := value.JSON([]byte(`{"data":{"id":42},"name":"x"}`))
	out := output.Extract(v, []output.Spec{
		{Name: "id", Path: "data.id"},
		{Name: "nm", Path: "name"},
		{Name: "missing", Path: "nope"}, // absent path is skipped, not an error
	})
	if len(out) != 2 {
		t.Fatalf("want 2 outputs, got %d (%v)", len(out), out)
	}
	if i, _ := out["id"].Int(); i != 42 {
		t.Fatalf("id=%v", i)
	}
	if s, _ := out["nm"].Str(); s != "x" {
		t.Fatalf("nm=%q", s)
	}
}
