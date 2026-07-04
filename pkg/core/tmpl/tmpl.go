// Package tmpl resolves {{ref}} / {{{ref}}} template tokens in a raw node
// definition against the node's input view (flow inputs + upstream outputs) and
// optional dynamic-variable generators, before the node is decoded. Refs use the
// same path addressing as assertions (value.Value.Get), so templates and
// assertions are symmetric. Nodes never see templates — they receive fully
// resolved, typed config.
package tmpl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

// token is the opening delimiter every template form starts with; a raw config
// without it needs no resolution at all.
//
//nolint:gochecknoglobals // immutable sentinel — avoids a per-call []byte alloc
var token = []byte("{{")

// rawPattern matches a whole-string {{{ref}}}: the token is the entire value, so
// it is replaced structurally (object/number/bool preserved), not stringified.
var rawPattern = regexp.MustCompile(`^\{\{\{\s*([^{}]+?)\s*\}\}\}$`)

// refPattern matches an inline {{ref}} for string interpolation.
var refPattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// DynFunc resolves a {{$name:arg:arg}} dynamic variable to a string. Nil disables
// dynamic variables (unresolved tokens are left verbatim).
type DynFunc func(name string, args []string) (string, error)

// uEscape marks a \u escape inside a JSON string literal — such a string could
// encode "{{" as {{, so it must be unquoted before the token check.
//
//nolint:gochecknoglobals // immutable sentinel — avoids a per-call []byte alloc
var uEscape = []byte(`\u`)

// Resolve substitutes template tokens in raw using the input view and dynamic-var
// resolver, returning resolved JSON. Unresolved refs are left verbatim so a typo
// is visible rather than silently empty.
//
// It rewrites the raw bytes directly: templates can only occur inside JSON
// string values, so everything outside string literals (and every string with no
// token) is copied verbatim, skipping the unmarshal-to-any/walk/remarshal round
// trip that dominated templated-node cost. Object keys are never templated
// (matching the previous walk, which only visited values).
func Resolve(raw json.RawMessage, view value.Value, dyn DynFunc) (json.RawMessage, error) {
	if !bytes.Contains(raw, token) {
		return raw, nil // no template tokens — nothing to rewrite
	}
	r := resolver{view: view, dyn: dyn}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		if raw[i] != '"' {
			out = append(out, raw[i])
			i++
			continue
		}
		end := stringEnd(raw, i)
		span := raw[i:end]
		i = end
		if isKey(raw, end) || (!bytes.Contains(span, token) && !bytes.Contains(span, uEscape)) {
			out = append(out, span...) // a key, or a string with no possible token
			continue
		}
		var s string
		if err := json.Unmarshal(span, &s); err != nil {
			return nil, fmt.Errorf("template parse: %w", err)
		}
		v := r.resolveString(s)
		if same, ok := v.(string); ok && same == s {
			out = append(out, span...) // untouched (e.g. an unresolved ref) — keep verbatim
			continue
		}
		enc, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("template remarshal: %w", err)
		}
		out = append(out, enc...)
	}
	return out, nil
}

// stringEnd returns the index just past the closing quote of the JSON string
// literal starting at start (raw[start] == '"'), honoring backslash escapes.
func stringEnd(raw []byte, start int) int {
	for i := start + 1; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			i++ // skip the escaped character
		case '"':
			return i + 1
		}
	}
	return len(raw) // unterminated — unreachable for the valid JSON flow.Parse produced
}

// isKey reports whether the string literal ending at i is an object key (the
// next non-space byte is ':'). Keys are copied verbatim, never templated.
func isKey(raw []byte, i int) bool {
	for ; i < len(raw); i++ {
		switch raw[i] {
		case ' ', '\t', '\n', '\r':
		default:
			return raw[i] == ':'
		}
	}
	return false
}

type resolver struct {
	view value.Value
	dyn  DynFunc
}

func (r resolver) resolveString(s string) any {
	if !strings.Contains(s, "{{") {
		return s // static string inside a templated node — skip both regex passes
	}
	if m := rawPattern.FindStringSubmatch(s); m != nil {
		if val, ok := r.value(m[1]); ok {
			return val
		}
		return s
	}
	return refPattern.ReplaceAllStringFunc(s, func(tok string) string {
		ref := refPattern.FindStringSubmatch(tok)[1]
		if val, ok := r.value(ref); ok {
			return value.Of(val).String()
		}
		return tok
	})
}

// value resolves a ref to its underlying value: a dynamic generator ($name:args)
// or a path into the input view ("nodeID.key" / bare flow input), using the same
// addressing as assertions.
func (r resolver) value(ref string) (any, bool) {
	ref = strings.TrimSpace(ref)
	if after, ok := strings.CutPrefix(ref, "$"); ok {
		if r.dyn == nil {
			return nil, false
		}
		name, args := parseDyn(after)
		s, err := r.dyn(name, args)
		if err != nil {
			return nil, false
		}
		return s, true
	}
	got, ok := r.view.Get(ref)
	if !ok {
		return nil, false
	}
	return got.Raw(), true
}

func parseDyn(s string) (string, []string) {
	parts := strings.Split(s, ":")
	return parts[0], parts[1:]
}
