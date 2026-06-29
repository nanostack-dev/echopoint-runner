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

// TestEquals_IsStringEquality pins the lenient coercion contract: equals (and
// the other string operators) compare stringified forms, so cross-type values
// with the same string form are equal. This is intentional, not type-aware
// equality — changing it is a behavior change visible to existing flows.
func TestEquals_IsStringEquality(t *testing.T) {
	cases := []struct {
		name           string
		actual, expect any
		want           bool
	}{
		{"int actual vs string expect", 200, "200", true},
		{"float actual vs string expect", 200.0, "200", true},
		{"bool actual vs string expect", true, "true", true},
		{"string vs string equal", "ok", "ok", true},
		{"different values", 200, "404", false},
		{"int vs string mismatch", 1, "one", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := operators.Compare(operators.OperatorTypeEquals, tc.actual, tc.expect)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("equals(%v, %v) = %v, want %v", tc.actual, tc.expect, got, tc.want)
			}
		})
	}
}

// TestIsKnown_MatchesComparators guards that IsKnown reports exactly the set of
// registered operators (so decode-time validation rejects neither more nor less
// than Compare does at runtime).
func TestIsKnown_MatchesComparators(t *testing.T) {
	builtins := []operators.OperatorType{
		operators.OperatorTypeEquals, operators.OperatorTypeNotEquals,
		operators.OperatorTypeContains, operators.OperatorTypeNotContains,
		operators.OperatorTypeStartsWith, operators.OperatorTypeEndsWith,
		operators.OperatorTypeRegex, operators.OperatorTypeEmpty,
		operators.OperatorTypeNotEmpty, operators.OperatorTypeGreaterThan,
		operators.OperatorTypeLessThan, operators.OperatorTypeGreaterThanOrEqual,
		operators.OperatorTypeLessThanOrEqual, operators.OperatorTypeBetween,
	}
	for _, op := range builtins {
		if !operators.IsKnown(op) {
			t.Errorf("IsKnown(%q) = false, want true (it is a built-in)", op)
		}
	}
	if operators.IsKnown("definitelyNotAnOperator") {
		t.Error("IsKnown should be false for an unregistered operator")
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
