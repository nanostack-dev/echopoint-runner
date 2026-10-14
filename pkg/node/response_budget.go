package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// ResponseBudget bounds the response body bytes that every request node sharing
// it holds in memory at once. A node reserves a body's bytes before reading it
// and returns them once its assertions and outputs are evaluated; a body that
// does not fit waits, bounded by its request's timeout, rather than allocating.
type ResponseBudget struct {
	sem   *semaphore.Weighted
	size  int64
	inUse atomic.Int64
}

// NewResponseBudget returns a budget of size bytes, or nil (no bound) when size
// is zero or less.
func NewResponseBudget(size int64) *ResponseBudget {
	if size <= 0 {
		return nil
	}
	return &ResponseBudget{sem: semaphore.NewWeighted(size), size: size}
}

// InUse reports the bytes currently reserved.
func (b *ResponseBudget) InUse() int64 {
	if b == nil {
		return 0
	}
	return b.inUse.Load()
}

type responseBudgetKey struct{}

// WithResponseBudget returns ctx carrying budget for the request nodes run on it.
func WithResponseBudget(ctx context.Context, budget *ResponseBudget) context.Context {
	if budget == nil {
		return ctx
	}
	return context.WithValue(ctx, responseBudgetKey{}, budget)
}

func responseBudgetFrom(ctx context.Context) *ResponseBudget {
	budget, _ := ctx.Value(responseBudgetKey{}).(*ResponseBudget)
	return budget
}

// budgetWaitError reports a request whose timeout elapsed while its response
// body waited for the budget as a timeout of that request.
func budgetWaitError(url string, err error) error {
	if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return spi.NewUserError(
		"REQUEST_TIMEOUT",
		fmt.Sprintf("Request to %s timed out waiting for runner memory to read its response", requestHost(url)),
		err,
	)
}

func (b *ResponseBudget) acquire(ctx context.Context, n int64) error {
	if err := b.sem.Acquire(ctx, n); err != nil {
		return fmt.Errorf("wait for %d bytes of the response budget: %w", n, err)
	}
	b.inUse.Add(n)
	return nil
}

func (b *ResponseBudget) release(n int64) {
	b.inUse.Add(-n)
	b.sem.Release(n)
}

// bodyWeight is what one kept body byte costs in the budget: the byte itself,
// and its share of the parsed form (a JSON tree or a text copy) the node builds
// from it.
const bodyWeight = 2

// bodyReservation is the part of a budget one response body holds. A nil
// reservation (no budget) is a valid no-op. Its sizes are in body bytes; it
// holds bodyWeight budget bytes for each.
type bodyReservation struct {
	budget *ResponseBudget
	mu     sync.Mutex
	held   int64
}

func reserveResponseBytes(ctx context.Context, n int64) (*bodyReservation, error) {
	budget := responseBudgetFrom(ctx)
	if budget == nil {
		return nil, nil //nolint:nilnil // no budget: nothing to reserve
	}
	reservation := &bodyReservation{budget: budget}
	if err := reservation.growTo(ctx, n); err != nil {
		return nil, err
	}
	return reservation, nil
}

// growTo waits until the reservation covers n body bytes, or holds the whole
// budget when they cost more: such a body is read alone.
func (r *bodyReservation) growTo(ctx context.Context, n int64) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n = min(n*bodyWeight, r.budget.size)
	if n <= r.held {
		return nil
	}
	if err := r.budget.acquire(ctx, n-r.held); err != nil {
		return err
	}
	r.held = n
	return nil
}

// shrinkTo returns what the reservation holds past n body bytes.
func (r *bodyReservation) shrinkTo(n int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n *= bodyWeight
	if n >= r.held {
		return
	}
	r.budget.release(r.held - n)
	r.held = n
}

// release returns every byte the reservation holds. It is safe to call twice.
func (r *bodyReservation) release() {
	r.shrinkTo(0)
}
