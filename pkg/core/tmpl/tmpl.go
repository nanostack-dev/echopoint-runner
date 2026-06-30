// Package tmpl resolves {{ref}} / {{{ref}}} template tokens in a raw node
// definition against the inter-node output bus, before the node is decoded.
// Nodes never see templates — they receive fully resolved, typed config.
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

// Bus maps node id -> that node's outputs; flow inputs live under the empty id.
type Bus map[string]value.Map

// Resolve substitutes template tokens in raw using the bus and returns resolved
// JSON. Unresolved refs are left verbatim so a typo is visible rather than
// silently empty.
func Resolve(raw json.RawMessage, bus Bus) (json.RawMessage, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("template parse: %w", err)
	}
	out, err := json.Marshal(walk(v, bus))
	if err != nil {
		return nil, fmt.Errorf("template remarshal: %w", err)
	}
	return out, nil
}

func walk(v any, bus Bus) any {
	switch t := v.(type) {
	case string:
		return resolveString(t, bus)
	case map[string]any:
		for k, val := range t {
			t[k] = walk(val, bus)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = walk(val, bus)
		}
		return t
	default:
		return v
	}
}

func resolveString(s string, bus Bus) any {
	if m := rawPattern.FindStringSubmatch(s); m != nil {
		if val, ok := lookup(m[1], bus); ok {
			return val.Raw()
		}
		return s
	}
	return refPattern.ReplaceAllStringFunc(s, func(tok string) string {
		ref := strings.TrimSpace(refPattern.FindStringSubmatch(tok)[1])
		if val, ok := lookup(ref, bus); ok {
			return val.String()
		}
		return tok
	})
}

func lookup(ref string, bus Bus) (value.Value, bool) {
	nodeID, key := splitRef(strings.TrimSpace(ref))
	outs, ok := bus[nodeID]
	if !ok {
		return value.Value{}, false
	}
	val, ok := outs[key]
	return val, ok
}

// splitRef splits "nodeID.key" into (nodeID, key); a bare "name" refers to a
// flow input, returning ("", name).
func splitRef(ref string) (string, string) {
	if before, after, found := strings.Cut(ref, "."); found {
		return before, after
	}
	return "", ref
}
