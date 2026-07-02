package nodes

import (
	"context"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// SetVariableCfg computes named values from templates. The engine resolves the
// templates before decode, so each variable here is already its final value —
// decoding boxes them directly (value.Map decodes in one pass). Declared
// assertions/outputs (on Base) run against the computed map via the engine
// post-step.
type SetVariableCfg struct {
	node.Base

	Variables value.Map `json:"variables"`
}

func runSetVariable(_ context.Context, cfg SetVariableCfg, _ value.Value, _ node.Runtime) (node.Result, error) {
	return node.Result{Outputs: cfg.Variables, Provided: true}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindSetVariable, runSetVariable) }
