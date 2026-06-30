// Package output extracts declared named outputs from a value. Like assert, it
// is a plain shared function the engine (or a looping node) calls.
package output

import "github.com/nanostack-dev/echopoint-runner/pkg/core/value"

// Spec is one declared output: bind Name to the value found at Path.
type Spec struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Extract pulls every spec's Path out of v into a named map. Missing paths are
// skipped — assertions, not outputs, enforce presence.
func Extract(v value.Value, specs []Spec) value.Map {
	out := make(value.Map, len(specs))
	for _, s := range specs {
		if got, ok := v.Get(s.Path); ok {
			out[s.Name] = got
		}
	}
	return out
}
