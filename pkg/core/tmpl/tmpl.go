// Package tmpl resolves {{ref}} / {{{ref}}} template tokens in a raw node
// definition against the inter-node output store (and optional dynamic-variable
// generators), before the node is decoded. Nodes never see templates — they
// receive fully resolved, typed config.
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

// Store maps node id -> that node's outputs; flow inputs live under the empty id.
type Store map[string]value.Map

// DynFunc resolves a {{$name:arg:arg}} dynamic variable to a string. Nil disables
// dynamic variables (unresolved tokens are left verbatim).
type DynFunc func(name string, args []string) (string, error)

// Resolve substitutes template tokens in raw using the store and dynamic-var
// resolver, returning resolved JSON. Unresolved refs are left verbatim so a typo
// is visible rather than silently empty.
func Resolve(raw json.RawMessage, store Store, dyn DynFunc) (json.RawMessage, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("template parse: %w", err)
	}
	r := resolver{store: store, dyn: dyn}
	out, err := json.Marshal(r.walk(v))
	if err != nil {
		return nil, fmt.Errorf("template remarshal: %w", err)
	}
	return out, nil
}

type resolver struct {
	store Store
	dyn   DynFunc
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
			return stringify(val)
		}
		return tok
	})
}

// value resolves a single ref to its underlying value: a dynamic generator
// ($name:args) or a store path (nodeID.key / bare flow input).
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
	nodeID, key := splitRef(ref)
	outs, ok := r.store[nodeID]
	if !ok {
		return nil, false
	}
	v, ok := outs[key]
	return v.Raw(), ok
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func parseDyn(s string) (string, []string) {
	parts := strings.Split(s, ":")
	return parts[0], parts[1:]
}

// splitRef splits "nodeID.key" into (nodeID, key); a bare "name" refers to a
// flow input, returning ("", name).
func splitRef(ref string) (string, string) {
	if before, after, found := strings.Cut(ref, "."); found {
		return before, after
	}
	return "", ref
}
