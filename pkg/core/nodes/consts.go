package nodes

import "encoding/json"

// Shared output-key names used by more than one node.
const (
	outKeyResults = "results"
	outKeyCount   = "count"
)

// jsonOrString parses raw as JSON when it is valid JSON, otherwise returns it as
// a string. This is the single JSON-or-raw fallback shared by request bodies and
// SSE event data, so the corner cases (null, numbers, empty) are decided once.
func jsonOrString(raw []byte) any {
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return v
	}
	return string(raw)
}
