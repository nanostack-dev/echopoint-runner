package nodes

import (
	"context"
	"net/http"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
)

// WallClock is the production node.Clock: real time, cancellation-aware sleep.
type WallClock struct{}

// Sleep waits for d or until ctx is cancelled, whichever comes first.
func (WallClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Now returns the current wall time.
func (WallClock) Now() time.Time { return time.Now() }

// DefaultRuntime wires the production effect implementations. Callers fill
// Subflow via engine.New and may override Vars.
func DefaultRuntime() node.Runtime {
	return node.Runtime{
		HTTP:  http.DefaultClient,
		Clock: WallClock{},
	}
}
