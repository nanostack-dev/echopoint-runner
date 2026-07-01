// Package value boxes decoded-JSON data behind a typed accessor API. It is the
// single place dynamic typing (any) lives in the runner; every other package is
// statically typed and reaches runtime data only through a value.Value.
package value

import (
	"encoding/json"
	"strings"

	"github.com/theory/jsonpath"
)

// Value boxes one decoded-JSON value. The zero Value is absent (see IsZero).
type Value struct{ raw any }

// Of boxes an arbitrary Go value.
func Of(v any) Value { return Value{raw: v} }

// JSON parses raw JSON bytes into a Value. Empty or invalid input yields None.
func JSON(b []byte) Value {
	if len(b) == 0 {
		return Value{}
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return Value{}
	}
	return Value{raw: v}
}

// Map is a node's named outputs.
type Map map[string]Value

// Value boxes the map as a single Value so a collection of named outputs can be
// asserted or extracted over by path — e.g. a child flow's outputs accessed as
// "{childNode}.{key}".
func (m Map) Value() Value {
	raw := make(map[string]any, len(m))
	for k, v := range m {
		raw[k] = v.raw
	}
	return Value{raw: raw}
}

// IsZero reports whether the value is absent.
func (v Value) IsZero() bool { return v.raw == nil }

// Raw returns the underlying value — the explicit escape hatch.
func (v Value) Raw() any { return v.raw }

// Str returns the value as a string when it is one.
func (v Value) Str() (string, bool) {
	s, ok := v.raw.(string)
	return s, ok
}

// Int returns the value as an int64 when it is numeric.
func (v Value) Int() (int64, bool) {
	switch n := v.raw.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// Bool returns the value as a bool when it is one.
func (v Value) Bool() (bool, bool) {
	b, ok := v.raw.(bool)
	return b, ok
}

// List returns the value as a slice of Values when it is a JSON array.
func (v Value) List() ([]Value, bool) {
	arr, ok := v.raw.([]any)
	if !ok {
		return nil, false
	}
	out := make([]Value, len(arr))
	for i := range arr {
		out[i] = Value{raw: arr[i]}
	}
	return out, true
}

// Get resolves an RFC-9535 JSONPath into the value, reporting whether it
// matched. A bare path ("body.id", "status") is treated as "$.body.id" for
// convenience; full JSONPath ("$.items[*].id", "$.data[?@.active]") also works.
// A single match returns that value; multiple matches return a list.
func (v Value) Get(path string) (Value, bool) {
	expr := path
	if !strings.HasPrefix(expr, "$") {
		expr = "$." + strings.TrimPrefix(expr, ".")
	}
	p, err := jsonpath.Parse(expr)
	if err != nil {
		return Value{}, false
	}
	nodes := p.Select(v.raw)
	switch len(nodes) {
	case 0:
		return Value{}, false
	case 1:
		return Value{raw: nodes[0]}, true
	default:
		out := make([]any, len(nodes))
		copy(out, nodes)
		return Value{raw: out}, true
	}
}

// String renders the value for comparison and interpolation.
func (v Value) String() string {
	switch t := v.raw.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// MarshalJSON renders the boxed value back to JSON (for results crossing the
// wire). Inbound boxing goes through JSON/Of, keeping every method a value
// receiver.
func (v Value) MarshalJSON() ([]byte, error) { return json.Marshal(v.raw) }
