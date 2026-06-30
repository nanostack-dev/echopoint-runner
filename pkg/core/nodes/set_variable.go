package nodes

import (
	"context"
	"encoding/json"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// SetVariableCfg computes named values from templates. The engine resolves the
// templates before decode, so each variable here is already its final value —
// the node just boxes them. Declared assertions/outputs (on Base) run against
// the computed map via the engine post-step.
type SetVariableCfg struct {
	node.Base

	Variables map[string]json.RawMessage `json:"variables"`
}

func runSetVariable(_ context.Context, cfg SetVariableCfg, _ value.Value, _ node.Runtime) (node.Result, error) {
	out := make(value.Map, len(cfg.Variables))
	for name, raw := range cfg.Variables {
		out[name] = value.JSON(raw)
	}
	return node.Result{Outputs: out, Assert: out.Value()}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindSetVariable, runSetVariable) }
