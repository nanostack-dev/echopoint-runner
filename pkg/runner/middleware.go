package runner

import (
	"context"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

// Retry re-runs a node up to attempts times while it returns an error, stopping
// early if the execution context is cancelled. attempts < 1 is treated as 1.
func Retry(attempts int) engine.Middleware {
	if attempts < 1 {
		attempts = 1
	}
	return func(next engine.NodeExecutor) engine.NodeExecutor {
		return func(ec node.ExecutionContext) (node.AnyExecutionResult, error) {
			var (
				result node.AnyExecutionResult
				err    error
			)
			for range attempts {
				result, err = next(ec)
				if err == nil {
					return result, nil
				}
				if ec.Context().Err() != nil {
					return result, err
				}
			}
			return result, err
		}
	}
}

// Timeout bounds each node execution to d, layered on top of the caller's
// context. A node that respects its context (HTTP request, delay) aborts when
// the deadline elapses.
func Timeout(d time.Duration) engine.Middleware {
	return func(next engine.NodeExecutor) engine.NodeExecutor {
		return func(ec node.ExecutionContext) (node.AnyExecutionResult, error) {
			ctx, cancel := context.WithTimeout(ec.Context(), d)
			defer cancel()
			ec.Ctx = ctx
			return next(ec)
		}
	}
}
