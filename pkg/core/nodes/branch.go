package nodes

import (
	"context"
	"encoding/json"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// BranchCase routes to Target when its condition holds over the branch target.
type BranchCase struct {
	When   assert.Spec `json:"when"`
	Target string      `json:"target"`
}

// BranchCfg routes execution to one successor by the first matching case (or
// Default). It evaluates over an explicit (templated) target, or the node's
// input context when none is given. A branch never fails on no-match — the
// engine skips the successors it routes away from (and their subtrees).
type BranchCfg struct {
	node.Base

	Target  json.RawMessage `json:"target"`
	Cases   []BranchCase    `json:"cases"`
	Default string          `json:"default"`
}

func runBranch(_ context.Context, cfg BranchCfg, in value.Value, _ node.Runtime) (node.Result, error) {
	target := in
	if len(cfg.Target) > 0 {
		target = value.JSON(cfg.Target)
	}
	matched, matchedIndex := "", -1
	for i, c := range cfg.Cases {
		if assert.Run(target, []assert.Spec{c.When}).AllPassed() {
			matched, matchedIndex = c.Target, i
			break
		}
	}
	if matched == "" {
		matched = cfg.Default
	}

	routed := []string{} // non-nil: a routing decision was made, even if to nothing
	if matched != "" {
		routed = []string{matched}
	}
	return node.Result{
		Outputs: value.Map{
			"matched":       value.Of(matched),
			"matched_index": value.Of(matchedIndex),
		},
		Routed: routed,
	}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindBranch, runBranch) }
