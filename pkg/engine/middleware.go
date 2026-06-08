package engine

import "github.com/nanostack-dev/echopoint-runner/pkg/node"

// NodeExecutor runs a single node and returns its result. It is the unit that
// Middleware wraps.
type NodeExecutor func(node.ExecutionContext) (node.AnyExecutionResult, error)

// Middleware wraps a NodeExecutor to add cross-cutting behavior — retry,
// timeout, tracing, circuit-breaking — without baking it into node code. The
// first middleware in the slice is the outermost wrapper (it runs first / sees
// the final result last).
type Middleware func(NodeExecutor) NodeExecutor

// chainMiddleware wraps base with the given middlewares, outermost-first.
func chainMiddleware(base NodeExecutor, middlewares []Middleware) NodeExecutor {
	wrapped := base
	for i := len(middlewares) - 1; i >= 0; i-- {
		if middlewares[i] != nil {
			wrapped = middlewares[i](wrapped)
		}
	}
	return wrapped
}
