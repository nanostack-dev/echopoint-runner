package assert_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

// TestOperators locks the full operator matrix the old runner supports.
func TestOperators(t *testing.T) {
	body := value.Of(map[string]any{
		"status": float64(201),
		"name":   "active-user",
		"tags":   []any{"a", "b"},
		"empty":  "",
	})
	cases := []struct {
		path, op, expected string
		want               bool
	}{
		{"status", "equals", "201", true}, // lenient int==string
		{"status", "equals", "200", false},
		{"name", "not_equals", "x", true},
		{"name", "contains", "active", true},
		{"name", "not_contains", "z", true},
		{"name", "starts_with", "active", true},
		{"name", "ends_with", "user", true},
		{"name", "regex", "^active-", true},
		{"empty", "empty", "", true},
		{"name", "not_empty", "", true},
		{"status", "gt", "200", true},
		{"status", "lt", "300", true},
		{"status", "gte", "201", true},
		{"status", "lte", "201", true},
		{"status", "between", "[200,299]", true},
		{"status", "between", "[100,200]", false},
		{"missing", "exists", "", false},
		{"status", "exists", "", true},
	}
	for _, c := range cases {
		spec := assert.Spec{Path: c.path, Op: assert.Op(c.op), Expected: json.RawMessage(quote(c.expected))}
		got := assert.Run(body, []assert.Spec{spec}).AllPassed()
		if got != c.want {
			t.Errorf("%s %s %q: got %v want %v", c.path, c.op, c.expected, got, c.want)
		}
	}
}

// quote renders a scalar test value as JSON: bare numbers/arrays stay literal,
// everything else becomes a JSON string.
func quote(s string) string {
	if s == "" {
		return `""`
	}
	if s[0] == '[' || (s[0] >= '0' && s[0] <= '9') {
		return s
	}
	b, _ := json.Marshal(s)
	return string(b)
}
