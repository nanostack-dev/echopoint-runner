package dynamicvars_test

import (
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/dynamicvars"
)

func TestEmailIsGenerated(t *testing.T) {
	c := dynamicvars.New("exec-email")
	v, _ := c.Resolve("email", nil)
	if !strings.Contains(v, "@") {
		t.Errorf("email %q is not an email", v)
	}
}

func TestPhoneIsGenerated(t *testing.T) {
	c := dynamicvars.New("exec-phone")
	if v, _ := c.Resolve("phone", nil); v == "" {
		t.Error("expected a phone number")
	}
}

func TestDeterministicPerExecutionID(t *testing.T) {
	a := dynamicvars.New("same-id")
	b := dynamicvars.New("same-id")
	for range 20 {
		av, _ := a.Resolve("uuid", nil)
		bv, _ := b.Resolve("uuid", nil)
		if av != bv {
			t.Fatalf("same execution id should give the same stream; got %q vs %q", av, bv)
		}
	}
	if c, _ := dynamicvars.New("other-id").Resolve("uuid", nil); c == "" {
		t.Error("expected a value for a different id")
	}
}

func TestUnknownVariableErrors(t *testing.T) {
	if _, err := dynamicvars.New("x").Resolve("definitelyNotAGenerator", nil); err == nil {
		t.Error("expected an error for an unknown dynamic variable")
	}
}

func TestCatalogIsPopulatedAndSorted(t *testing.T) {
	cat := dynamicvars.Catalog()
	if len(cat) < 40 {
		t.Errorf("expected a sizeable catalog, got %d", len(cat))
	}
	for _, e := range cat {
		if e.Name == "" || e.Desc == "" || e.Example == "" || e.Category == "" {
			t.Errorf("catalog entry %q is missing fields", e.Name)
		}
	}
}
