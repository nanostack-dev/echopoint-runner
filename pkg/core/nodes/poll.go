package nodes

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const (
	defaultPollAttempts = 10
	defaultPollInterval = time.Second
)

// PollCfg configures a poll-until node: re-run an inline body flow until the
// node's assertions (the exit conditions) all pass on a single attempt, or the
// attempt / deadline budget is exhausted. This is the self-evaluating case — the
// node calls assert.Run itself per attempt and returns Assert:None so the engine
// skips its post-step.
type PollCfg struct {
	node.Base

	// Body is the inline flow polled each attempt (re-templated per attempt with
	// the injected "attempt"). NOTE: the outer template pass also visits it, so a
	// flow input or node named "attempt" could be substituted before polling —
	// avoid that collision.
	Body        json.RawMessage `json:"body"`
	MaxAttempts int             `json:"max_attempts"`
	IntervalMs  int64           `json:"interval_ms"`
	TimeoutMs   int64           `json:"timeout_ms"`
}

func runPoll(ctx context.Context, cfg PollCfg, _ value.Value, rt node.Runtime) (node.Result, error) {
	if len(cfg.Assertions) == 0 {
		return node.Result{}, node.UserErrf("POLL_FAILED", "poll needs at least one exit condition")
	}
	body, err := flow.Parse(cfg.Body)
	if err != nil {
		return node.Result{}, node.UserErrf("POLL_FAILED", "poll body: %v", err)
	}

	attempts := cfg.MaxAttempts
	if attempts <= 0 {
		attempts = defaultPollAttempts
	}
	interval := defaultPollInterval
	if cfg.IntervalMs > 0 {
		interval = time.Duration(cfg.IntervalMs) * time.Millisecond
	}
	if cfg.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		out, runErr := rt.Subflow.RunInline(ctx, body, value.Map{"attempt": value.Of(attempt)})
		if runErr != nil {
			return node.Result{}, node.UserErrf(
				"POLL_BODY_FAILED",
				"poll body failed on attempt %d: %v",
				attempt,
				runErr,
			)
		}
		if assert.Run(out.Value(), cfg.Assertions).AllPassed() {
			return node.Result{Outputs: value.Map{
				"attempts": value.Of(attempt),
				"result":   out.Value(),
			}}, nil
		}
		if attempt < attempts {
			if sleepErr := rt.Clock.Sleep(ctx, interval); sleepErr != nil {
				return node.Result{}, sleepErr
			}
		}
	}
	return node.Result{}, node.UserErrf(
		"POLL_CONDITION_NOT_MET",
		"poll exit condition not met after %d attempts",
		attempts,
	)
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindPoll, runPoll) }
