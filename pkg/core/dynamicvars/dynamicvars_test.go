package dynamicvars_test

import (
	"strconv"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/dynamicvars"
)

// TestCatalogGenerators proves every catalogued generator resolves to a
// non-empty value with no error — the breadth port from the old runner.
func TestCatalogGenerators(t *testing.T) {
	c := dynamicvars.New("exec-1")
	cat := dynamicvars.Catalog()
	if len(cat) < 50 {
		t.Fatalf("catalog should carry the full generator set, got %d", len(cat))
	}
	for _, e := range cat {
		v, err := c.Resolve(e.Name, nil)
		if err != nil {
			t.Errorf("%s: %v", e.Name, err)
		}
		if v == "" {
			t.Errorf("%s: empty value", e.Name)
		}
	}
}

// TestDeterministicSeeding proves two contexts built from the same execution id
// produce the same stream of values (reproducible runs), and different ids differ.
func TestDeterministicSeeding(t *testing.T) {
	names := []string{"firstName", "email", "uuid", "int", "city"}
	seq := func(id string) []string {
		c := dynamicvars.New(id)
		out := make([]string, len(names))
		for i, n := range names {
			out[i], _ = c.Resolve(n, nil)
		}
		return out
	}
	a, b, other := seq("exec-42"), seq("exec-42"), seq("exec-99")
	for i := range names {
		if a[i] != b[i] {
			t.Fatalf("same execution id must replay %q: %q != %q", names[i], a[i], b[i])
		}
	}
	same := true
	for i := range names {
		if a[i] != other[i] {
			same = false
		}
	}
	if same {
		t.Fatal("different execution ids should produce different streams")
	}
}

// TestRunIDStable proves runId is identical across draws within one execution.
func TestRunIDStable(t *testing.T) {
	c := dynamicvars.New("exec-7")
	first, _ := c.Resolve("runId", nil)
	second, _ := c.Resolve("runId", nil)
	if first == "" || first != second {
		t.Fatalf("runId must be stable within a run: %q vs %q", first, second)
	}
}

// TestArgs proves colon-separated args are honored.
func TestArgs(t *testing.T) {
	c := dynamicvars.New("exec-args")
	got, err := c.Resolve("int", []string{"5", "5"}) // min==max==5
	if err != nil {
		t.Fatal(err)
	}
	if got != "5" {
		t.Fatalf("int:5:5 should be 5, got %q", got)
	}
	digits, _ := c.Resolve("digits", []string{"4"})
	if len(digits) != 4 {
		t.Fatalf("digits:4 should be 4 chars, got %q", digits)
	}
	if _, e := strconv.Atoi(digits); e != nil {
		t.Fatalf("digits should be numeric, got %q", digits)
	}
}

// TestUnknownVarErrors proves an unregistered name is a clear error.
func TestUnknownVarErrors(t *testing.T) {
	c := dynamicvars.New("exec-x")
	if _, err := c.Resolve("nope", nil); err == nil {
		t.Fatal("unknown dynamic variable should error")
	}
}

// TestEphemeralWorks proves the no-execution-id constructor still resolves.
func TestEphemeralWorks(t *testing.T) {
	c := dynamicvars.NewEphemeral()
	if v, err := c.Resolve("uuid", nil); err != nil || v == "" {
		t.Fatalf("ephemeral uuid: %q %v", v, err)
	}
}
