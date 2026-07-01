package nodes

import (
	"context"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// AssertCfg validates data with no target of its own: its assertions address the
// input context directly by fully-qualified path — flow inputs by name, any
// already-executed node's outputs as "nodeID.key". References are checked to
// exist, so a typo or an unexecuted node is a clear error.
type AssertCfg struct {
	node.Base
}

func runAssert(_ context.Context, cfg AssertCfg, in value.Value, _ node.Runtime) (node.Result, error) {
	if err := requireRefs(in, cfg.Assertions); err != nil {
		return node.Result{}, err
	}
	return node.Result{Assert: in, Provided: true}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindAssert, runAssert) }
