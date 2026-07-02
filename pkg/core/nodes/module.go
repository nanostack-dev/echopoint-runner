package nodes

import (
	"context"
	"encoding/json"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// ModuleCfg configures a module node: run a child flow once with the given
// inputs. It is the simplest composite node — poll and loop wrap the same
// RunSubflow/RunInline call in control flow.
type ModuleCfg struct {
	node.Base

	Body   string                     `json:"body_flow_id"`
	Inputs map[string]json.RawMessage `json:"inputs"`
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
	inputs := make(value.Map, len(cfg.Inputs))
	for k, raw := range cfg.Inputs {
		inputs[k] = value.JSON(raw) // templates already resolved by the engine
	}
	out, err := rt.Subflow.RunSubflow(ctx, cfg.Body, inputs)
	if err != nil {
		return node.Result{}, err
	}
	return node.Result{Outputs: out}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindModule, runModule) }
