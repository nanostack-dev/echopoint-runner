package nodes

import (
	"context"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// ModuleCfg configures a module node: run a child flow once with the given
// inputs. It is the simplest composite node — poll and loop wrap the same
// RunSubflow/RunInline call in control flow. Inputs decode straight into a
// value.Map (templates were already resolved by the engine).
type ModuleCfg struct {
	node.Base

	Body   string    `json:"body_flow_id"`
	Inputs value.Map `json:"inputs"`
}

// ReferencedFlows implements node.FlowReferencer: the child flow this module
// runs, so the engine validates the reference and detects cycles generically.
func (c ModuleCfg) ReferencedFlows() []string {
	if c.Body == "" {
		return nil
	}
	return []string{c.Body}
}

func runModule(ctx context.Context, cfg ModuleCfg, _ value.Value, rt node.Runtime) (node.Result, error) {
	out, err := rt.Subflow.RunSubflow(ctx, cfg.Body, cfg.Inputs)
	if err != nil {
		return node.Result{}, err
	}
	return node.Result{Outputs: out}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindModule, runModule) }
