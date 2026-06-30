// Package value boxes decoded-JSON data behind a typed accessor API. It is the
// single place dynamic typing (any) lives in the runner; every other package is
// statically typed and reaches runtime data only through a value.Value.
package value

import (
	"encoding/json"
	"strconv"
	"strings"
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

// Get walks a dotted/JSONPath-lite path ("$.a.b", "a.0.b") into the value,
// reporting whether the path resolved. A full RFC-9535 JSONPath drops in here
// later; this covers object/array member access.
func (v Value) Get(path string) (Value, bool) {
	cur := v.raw
	for _, seg := range splitPath(path) {
		switch c := cur.(type) {
		case map[string]any:
			next, ok := c[seg]
			if !ok {
				return Value{}, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(c) {
				return Value{}, false
			}
			cur = c[i]
		default:
			return Value{}, false
		}
	}
	return Value{raw: cur}, true
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

func splitPath(path string) []string {
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}
