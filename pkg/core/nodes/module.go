package nodes

import (
	"context"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// ModuleCfg configures a module node: run a child flow once. It is the simplest
// composite node — poll and loop wrap the same RunSubflow call in control flow.
type ModuleCfg struct {
	node.Base

	Body string `json:"body_flow_id"`
}

func runModule(ctx context.Context, cfg ModuleCfg, rt node.Runtime) (node.Result, error) {
	out, err := rt.Subflow.RunSubflow(ctx, cfg.Body, value.Map{})
	if err != nil {
		return node.Result{}, err
	}
	return node.Result{Outputs: out}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindModule, runModule) }
