package nodes

import (
	"context"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// DelayCfg configures a delay node.
type DelayCfg struct {
	node.Base

	DurationMs int64 `json:"duration_ms"`
}

func runDelay(ctx context.Context, cfg DelayCfg, rt node.Runtime) (node.Result, error) {
	if err := rt.Clock.Sleep(ctx, time.Duration(cfg.DurationMs)*time.Millisecond); err != nil {
		return node.Result{}, err
	}
	return node.Result{Outputs: value.Map{"delayed_ms": value.Of(cfg.DurationMs)}}, nil
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindDelay, runDelay) }
