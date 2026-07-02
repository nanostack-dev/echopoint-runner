package engine

import (
	"context"
	"slices"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
)

// NodeExec is a node's run-and-assert unit — the innermost thing middleware
// wraps. Wrapping this (rather than just Execute) means retry re-runs the
// assertion pass too, matching the old runner's semantics.
type NodeExec func(ctx context.Context) (node.Result, assert.Results, error)

// Middleware wraps a NodeExec. The engine chains them outermost-first around
// every node.
type Middleware func(NodeExec) NodeExec

func chainMiddleware(base NodeExec, mws []Middleware) NodeExec {
	for _, mw := range slices.Backward(mws) {
		base = mw(base)
	}
	return base
}

// Retry re-runs a node up to attempts times while it errors (including on an
// assertion failure). attempts <= 1 disables retry.
func Retry(attempts int) Middleware {
	return func(next NodeExec) NodeExec {
		return func(ctx context.Context) (node.Result, assert.Results, error) {
			var (
				res node.Result
				ar  assert.Results
				err error
			)
			for range max(attempts, 1) {
				if res, ar, err = next(ctx); err == nil {
					return res, ar, nil
				}
			}
			return res, ar, err
		}
	}
}

// Timeout bounds each node's execution with a per-node deadline.
func Timeout(d time.Duration) Middleware {
	return func(next NodeExec) NodeExec {
		return func(ctx context.Context) (node.Result, assert.Results, error) {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx)
		}
	}
}
