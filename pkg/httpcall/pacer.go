package httpcall

import (
	"context"
	"fmt"
	"time"
)

// Pacer spaces request starts across every execution sharing it. Idle time
// never accumulates burst capacity; the first request can start immediately.
type Pacer struct {
	gate     chan struct{}
	interval time.Duration
	last     time.Time
}

// NewPacer returns nil (no pacing) when interval is zero or less.
func NewPacer(interval time.Duration) *Pacer {
	if interval <= 0 {
		return nil
	}
	return &Pacer{gate: make(chan struct{}, 1), interval: interval}
}

type pacerKey struct{}

// WithPacer shares pacer with all HTTP calls made on ctx, including child flows.
func WithPacer(ctx context.Context, pacer *Pacer) context.Context {
	if pacer == nil {
		return ctx
	}
	return context.WithValue(ctx, pacerKey{}, pacer)
}

// Wait waits for the next request start, or returns the context error. A
// cancelled waiter consumes no future slot and is never recorded as a call.
func Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pacer, _ := ctx.Value(pacerKey{}).(*Pacer)
	if pacer == nil {
		return nil
	}
	select {
	case pacer.gate <- struct{}{}:
		defer func() { <-pacer.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if delay := time.Until(pacer.last.Add(pacer.interval)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return fmt.Errorf("wait for request pacing: %w", ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pacer.last = time.Now()
	return nil
}
