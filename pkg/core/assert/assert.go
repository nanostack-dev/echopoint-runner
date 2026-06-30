// Package assert evaluates declared assertions against a value. It is shared:
// the engine runs it as a post-step for provider nodes, and looping nodes
// (poll, sse) call it per attempt/event. It is a plain package function, not a
// method on a context — the way json.Marshal is.
package assert

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

// Spec is one declared assertion: extract Path from the value, compare it with
// Op against Expected. Expected stays raw JSON until evaluation, where it is
// boxed into a value.Value — so value.Value needs no custom unmarshaler.
type Spec struct {
	Path     string          `json:"path"`
	Op       Op              `json:"op"`
	Expected json.RawMessage `json:"expected"`
}

// Op is a comparison operator.
type Op string

// Built-in operators.
const (
	OpEquals      Op = "equals"
	OpNotEquals   Op = "not_equals"
	OpContains    Op = "contains"
	OpGreaterThan Op = "gt"
	OpLessThan    Op = "lt"
	OpExists      Op = "exists"
)

// Result is one assertion's outcome.
type Result struct {
	Spec   Spec
	Actual value.Value
	Passed bool
	Err    string
}

// Results is the outcome of evaluating a set of specs.
type Results []Result

// AllPassed reports whether every assertion passed.
func (rs Results) AllPassed() bool {
	for _, r := range rs {
		if !r.Passed {
			return false
		}
	}
	return true
}

// AnyFailed reports whether any assertion failed.
func (rs Results) AnyFailed() bool { return !rs.AllPassed() }

// Run evaluates every spec against v and returns one Result each.
func Run(v value.Value, specs []Spec) Results {
	out := make(Results, 0, len(specs))
	for _, s := range specs {
		out = append(out, eval(v, s))
	}
	return out
}

func eval(v value.Value, s Spec) Result {
	actual, found := v.Get(s.Path)
	r := Result{Spec: s, Actual: actual}
	if s.Op == OpExists {
		r.Passed = found
		return r
	}
	if !found {
		r.Err = fmt.Sprintf("path %q not found", s.Path)
		return r
	}
	passed, err := compare(s.Op, actual, value.JSON(s.Expected))
	if err != nil {
		r.Err = err.Error()
		return r
	}
	r.Passed = passed
	return r
}

func compare(op Op, actual, expected value.Value) (bool, error) {
	switch op {
	case OpEquals:
		return actual.String() == expected.String(), nil
	case OpNotEquals:
		return actual.String() != expected.String(), nil
	case OpContains:
		return strings.Contains(actual.String(), expected.String()), nil
	case OpGreaterThan, OpLessThan:
		a, err1 := toFloat(actual)
		e, err2 := toFloat(expected)
		if err1 != nil || err2 != nil {
			return false, fmt.Errorf("operator %q needs numbers", op)
		}
		if op == OpGreaterThan {
			return a > e, nil
		}
		return a < e, nil
	case OpExists:
		return true, nil
	}
	return false, fmt.Errorf("unknown operator %q", op)
}

func toFloat(v value.Value) (float64, error) {
	if i, ok := v.Int(); ok {
		return float64(i), nil
	}
	return strconv.ParseFloat(v.String(), 64)
}
