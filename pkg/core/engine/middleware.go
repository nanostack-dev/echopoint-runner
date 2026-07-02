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

// retryBackoff is the base gap before a retry; attempt i waits (i+1)*base. Kept
// small and linear — enough to not hammer a flaky endpoint back-to-back.
const retryBackoff = 100 * time.Millisecond

// Retry re-runs a node up to attempts times while it errors (including on an
// assertion failure), pausing a short ctx-respecting backoff between attempts.
// attempts <= 1 disables retry.
func Retry(attempts int) Middleware {
	return func(next NodeExec) NodeExec {
		return func(ctx context.Context) (node.Result, assert.Results, error) {
			var (
				res node.Result
				ar  assert.Results
				err error
			)
			n := max(attempts, 1)
			for i := range n {
				if res, ar, err = next(ctx); err == nil {
					return res, ar, nil
				}
				if i == n-1 {
					break
				}
				select {
				case <-ctx.Done(): // cancelled: stop retrying, surface the last error
					return res, ar, err
				case <-time.After(time.Duration(i+1) * retryBackoff):
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
