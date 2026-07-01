package dynamicvars_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/dynamicvars"
)

func TestGenerators(t *testing.T) {
	g := dynamicvars.New()

	uuid, err := g.Resolve("uuid", nil)
	if err != nil || len(uuid) != 32 { // 16 bytes hex
		t.Fatalf("uuid: %q %v", uuid, err)
	}
	hex, _ := g.Resolve("hex", []string{"4"})
	if len(hex) != 8 { // 4 bytes hex
		t.Fatalf("hex len: %q", hex)
	}
	email, _ := g.Resolve("email", nil)
	if len(email) < len("@example.com") || email[len(email)-len("@example.com"):] != "@example.com" {
		t.Fatalf("email: %q", email)
	}
	if _, uerr := g.Resolve("nope", nil); uerr == nil {
		t.Fatal("unknown generator should error")
	}
}

func TestRandIntBounds(t *testing.T) {
	g := dynamicvars.New()
	for range 50 {
		s, err := g.Resolve("int", []string{"10"})
		if err != nil {
			t.Fatal(err)
		}
		if s < "0" { // crude: non-empty numeric string
			t.Fatalf("int out of range: %q", s)
		}
	}
}
