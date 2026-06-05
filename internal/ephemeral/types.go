package ephemeral

import (
	"encoding/json"
	"time"

	flowpkg "github.com/nanostack-dev/echopoint-runner/pkg/flow"
)

// Package is the ephemeral execution bundle the server returns on launch and
// the CLI feeds to the runner via stdin or a file.  It matches the server's
// ephemeral_runner.package shape exactly.
// IMPORTANT: inputs may contain secrets – never log values, only key names.
type Package struct {
	ExecutionID     string                         `json:"execution_id"`
	FlowID          string                         `json:"flow_id"`
	FlowDefinition  json.RawMessage                `json:"flow_definition"`
	Inputs          map[string]any                 `json:"inputs"`
	ReferencedFlows flowpkg.ReferencedFlowRegistry `json:"referenced_flows,omitempty"`
}

// Result is the ephemeral execution result the runner writes to stdout or a
// file, and the CLI POSTs as the ephemeral-result publication body.  It
// matches the server's ephemeral result endpoint request shape exactly.
type Result struct {
	Status       string          `json:"status"`
	StartedAt    time.Time       `json:"started_at"`
	CompletedAt  time.Time       `json:"completed_at"`
	DurationMs   int64           `json:"duration_ms"`
	Result       *map[string]any `json:"result"`
	ErrorMessage *string         `json:"error_message,omitempty"`
	ErrorCode    *string         `json:"error_code,omitempty"`
}
