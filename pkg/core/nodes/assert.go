package nodes

import (
	"context"
	"encoding/json"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// AssertCfg validates a target value. With an explicit (templated) target it
// asserts over that; with no target it asserts over the node's input context
// (flow inputs + upstream outputs). The engine runs the declared assertions and
// flips the node to failed on a miss.
type AssertCfg struct {
	node.Base

	Target json.RawMessage `json:"target"`
}

func runAssert(_ context.Context, cfg AssertCfg, in value.Value, _ node.Runtime) (node.Result, error) {
	target := in
	if len(cfg.Target) > 0 {
		target = value.JSON(cfg.Target)
	}
	return node.Result{Assert: target}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindAssert, runAssert) }
