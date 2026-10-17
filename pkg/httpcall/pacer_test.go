package httpcall_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/httpcall"
)

func TestPacerSpacesConcurrentStarts(t *testing.T) {
	ctx := httpcall.WithPacer(context.Background(), httpcall.NewPacer(30*time.Millisecond))
	starts := make(chan time.Time, 5)
	var workers sync.WaitGroup
	for range cap(starts) {
		workers.Go(func() {
			if err := httpcall.Wait(ctx); err != nil {
				t.Error(err)
			}
			starts <- time.Now()
		})
	}
	workers.Wait()
	close(starts)
	var times []time.Time
	for started := range starts {
		times = append(times, started)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	assert.GreaterOrEqual(t, times[len(times)-1].Sub(times[0]), 120*time.Millisecond)
}

func TestPacerCancellationDoesNotReserveFutureSlots(t *testing.T) {
	ctx := httpcall.WithPacer(context.Background(), httpcall.NewPacer(100*time.Millisecond))
	require.NoError(t, httpcall.Wait(ctx))
	deadlineCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, httpcall.Wait(deadlineCtx), context.DeadlineExceeded)
	// A cancelled wait does not push the next request back by another interval.
	nextCtx, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	defer stop()
	require.NoError(t, httpcall.Wait(nextCtx))
}

func TestPacerDisabledAndAlreadyCancelled(t *testing.T) {
	ctx := httpcall.WithPacer(context.Background(), httpcall.NewPacer(0))
	for range 10 {
		require.NoError(t, httpcall.Wait(ctx))
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, httpcall.Wait(cancelled), context.Canceled)
}
