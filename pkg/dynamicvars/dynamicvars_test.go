package dynamicvars_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/dynamicvars"
)

// ibanMod97Valid reports whether an IBAN passes the ISO 13616 mod-97 check.
func ibanMod97Valid(iban string) bool {
	rearranged := iban[4:] + iban[:4]
	remainder := 0
	for _, r := range rearranged {
		var v int
		switch {
		case r >= '0' && r <= '9':
			v = int(r - '0')
		case r >= 'A' && r <= 'Z':
			v = int(r-'A') + 10
		default:
			return false
		}
		if v >= 10 {
			remainder = (remainder*100 + v) % 97
		} else {
			remainder = (remainder*10 + v) % 97
		}
	}
	return remainder == 1
}

func TestIBANIsModulo97Valid(t *testing.T) {
	c := dynamicvars.New("exec-iban-test")
	for _, country := range []string{"FR", "DE", "GB", "ES", "IT", "NL", "BE", "CH"} {
		v, err := c.Resolve("iban", []string{country})
		if err != nil {
			t.Fatalf("%s: %v", country, err)
		}
		if !strings.HasPrefix(v, country) {
			t.Errorf("%s: iban %q has wrong country prefix", country, v)
		}
		if !ibanMod97Valid(v) {
			t.Errorf("%s: iban %q fails mod-97", country, v)
		}
	}
}

func TestEmailUsesReservedDomain(t *testing.T) {
	c := dynamicvars.New("exec-email")
	v, _ := c.Resolve("email", nil)
	if !strings.HasSuffix(v, "@example.test") {
		t.Errorf("email %q should use the reserved @example.test domain", v)
	}
}

func TestPhoneIsE164(t *testing.T) {
	c := dynamicvars.New("exec-phone")
	v, _ := c.Resolve("phone", nil)
	if !regexp.MustCompile(`^\+\d{8,15}$`).MatchString(v) {
		t.Errorf("phone %q is not E.164", v)
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
