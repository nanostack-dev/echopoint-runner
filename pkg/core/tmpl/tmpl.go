// Package tmpl resolves {{ref}} / {{{ref}}} template tokens in a raw node
// definition against the node's input view (flow inputs + upstream outputs) and
// optional dynamic-variable generators, before the node is decoded. Refs use the
// same path addressing as assertions (value.Value.Get), so templates and
// assertions are symmetric. Nodes never see templates — they receive fully
// resolved, typed config.
package tmpl

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

// rawPattern matches a whole-string {{{ref}}}: the token is the entire value, so
// it is replaced structurally (object/number/bool preserved), not stringified.
var rawPattern = regexp.MustCompile(`^\{\{\{\s*([^{}]+?)\s*\}\}\}$`)

// refPattern matches an inline {{ref}} for string interpolation.
var refPattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// DynFunc resolves a {{$name:arg:arg}} dynamic variable to a string. Nil disables
// dynamic variables (unresolved tokens are left verbatim).
type DynFunc func(name string, args []string) (string, error)

// Resolve substitutes template tokens in raw using the input view and dynamic-var
// resolver, returning resolved JSON. Unresolved refs are left verbatim so a typo
// is visible rather than silently empty.
func Resolve(raw json.RawMessage, view value.Value, dyn DynFunc) (json.RawMessage, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("template parse: %w", err)
	}
	r := resolver{view: view, dyn: dyn}
	out, err := json.Marshal(r.walk(v))
	if err != nil {
		return nil, fmt.Errorf("template remarshal: %w", err)
	}
	return out, nil
}

type resolver struct {
	view value.Value
	dyn  DynFunc
}

func (r resolver) walk(v any) any {
	switch t := v.(type) {
	case string:
		return r.resolveString(t)
	case map[string]any:
		for k, val := range t {
			t[k] = r.walk(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = r.walk(val)
		}
		return t
	default:
		return v
	}
}

func (r resolver) resolveString(s string) any {
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
