package controlplane

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	defaultBackoffInitial = 200 * time.Millisecond
	defaultBackoffMax     = 5 * time.Second
	// backoffGrowth multiplies the wait after each failed attempt.
	backoffGrowth = 2
	// jitterShare is the inverse of the largest share of a wait taken off at random.
	jitterShare = 2
)

// Backoff spaces the attempts of a control-plane call that failed transiently:
// the first retry waits Initial, each later one twice the previous, up to Max.
// A zero field takes the default of 200 ms doubling to 5 s.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
}

// delay is the wait before retry n (from 1), less a random part of up to half,
// so runners that failed together do not retry together.
func (b Backoff) delay(retry int) time.Duration {
	initial, maximum := b.Initial, b.Max
	if initial <= 0 {
		initial = defaultBackoffInitial
	}
	if maximum <= 0 {
		maximum = defaultBackoffMax
	}
	wait := initial
	for range retry - 1 {
		if wait > maximum/backoffGrowth {
			wait = maximum
			break
		}
		wait *= backoffGrowth
	}
	wait = min(wait, maximum)
	return wait - randomBelow(wait/jitterShare)
}

// randomBelow returns a random duration in [0, limit).
func randomBelow(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64())
}

// RetryTransient calls attempt until it succeeds, fails with an error that
// IsTransient rejects, or ctx ends, waiting by backoff between attempts. The
// deadline of ctx is the whole retry budget; the HTTP client bounds each
// attempt. It returns the last attempt's error, joined to the one before it
// when the budget ended during that attempt.
func RetryTransient(
	ctx context.Context,
	backoff Backoff,
	operation string,
	attempt func(context.Context) error,
) error {
	var previous error
	for retry := 1; ; retry++ {
		err := attempt(ctx)
		if err == nil || !IsTransient(err) {
			return err
		}
		if ctx.Err() != nil {
			if previous != nil {
				return fmt.Errorf("%w; retry budget ended: %w", previous, err)
			}
			return err
		}
		previous = err
		wait := backoff.delay(retry)
		log.Warn().
			Err(err).
			Str("operation", operation).
			Int("attempt", retry).
			Dur("retry_in", wait).
			Msg("runner control-plane request failed, retrying")
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}
