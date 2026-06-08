package operators_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/operators"
)

func TestCompare_BuiltIns(t *testing.T) {
	// Lenient coercion: typed actual (int) vs string expected.
	if ok, err := operators.Compare(operators.OperatorTypeEquals, 200, "200"); err != nil || !ok {
		t.Errorf("equals 200==\"200\" should pass, got ok=%v err=%v", ok, err)
	}
	if ok, err := operators.Compare(operators.OperatorTypeGreaterThan, 5, "3"); err != nil || !ok {
		t.Errorf("greaterThan 5>3 should pass, got ok=%v err=%v", ok, err)
	}
	if ok, _ := operators.Compare(operators.OperatorTypeBetween, 5, []any{1, 10}); !ok {
		t.Error("between 5 in [1,10] should pass")
	}
	if _, err := operators.Compare("nope", 1, 1); err == nil {
		t.Error("unknown operator must error")
	}
}

func TestRegister_AddsOperator(t *testing.T) {
	const custom operators.OperatorType = "isFortyTwo"
	operators.Register(custom, func(actual, _ any) (bool, error) {
		return actual == 42, nil
	})
	if ok, err := operators.Compare(custom, 42, nil); err != nil || !ok {
		t.Errorf("registered operator should be usable, got ok=%v err=%v", ok, err)
	}
}
