// Package result is the outcome of a flow run: a per-node record plus the
// flow-level verdict. The engine records failures here and keeps going (skipping
// dependents) rather than returning a Go error, so a caller sees the whole run.
package result

import (
	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Status is a node's terminal state.
type Status string

const (
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// Skip reasons — a wire contract with echopoint; the exact strings must not drift.
const (
	SkipDependencyFailed  = "dependency_failed"
	SkipDependencySkipped = "dependency_skipped"
	SkipRoutedAway        = "routed_away_by_branch"
	SkipAbortedAfterFail  = "aborted_after_failure"
	SkipMissingInputs     = "missing_inputs"
	SkipNotReachable      = "not_reachable_after_main_phase"
)

// NodeResult records one node's outcome.
type NodeResult struct {
	ID         string
	Kind       spi.Kind
	Status     Status
	Outputs    value.Map
	Assertions assert.Results
	Error      string
	Code       string
	SkipReason string
}

// FlowResult is a whole run's outcome. Success is false if any on_success node
// failed or the flow failed validation.
type FlowResult struct {
	Success bool
	Nodes   map[string]*NodeResult
	Outputs value.Map
	Error   string
	Code    string
}

// Node returns a node's result (nil if absent).
func (f *FlowResult) Node(id string) *NodeResult { return f.Nodes[id] }
