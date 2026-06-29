package node_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func TestAs_MatchesConcreteType(t *testing.T) {
	var result spi.AnyResult = &node.RequestExecutionResult{}

	if got, ok := spi.As[*node.RequestExecutionResult](result); !ok || got == nil {
		t.Fatalf("As to the matching type should succeed, got ok=%v", ok)
	}
	if _, ok := spi.As[*node.DelayExecutionResult](result); ok {
		t.Fatal("As to a non-matching type should report false")
	}
}

func TestMustAs_PanicsOnMismatch(t *testing.T) {
	var result spi.AnyResult = &node.RequestExecutionResult{}

	if spi.MustAs[*node.RequestExecutionResult](result) == nil {
		t.Fatal("MustAs to the matching type should return the value")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("MustAs to a non-matching type should panic")
		}
	}()
	spi.MustAs[*node.DelayExecutionResult](result)
}
