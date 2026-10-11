// Package httpcall records the outbound HTTP calls of an execution and
// enforces the HTTP call limit.
//
// Two values travel in the execution context. A Limiter, put there by
// runner.Run, counts every call of the execution against the limit. A
// Collector, put there by the engine around each top-level node, gathers that
// node's calls into its result. Loop, poll and module bodies run on the
// node's context, so their calls count against the same limit and land in the
// same node result.
package httpcall

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// LimitExceededCode is the error code of the node whose call passed the limit.
const LimitExceededCode = "HTTP_CALL_LIMIT_EXCEEDED"

type (
	limiterKey   struct{}
	collectorKey struct{}
)

// Limiter counts the calls of one execution. It is safe for concurrent use.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	started int
}

// NewLimiter returns a limiter that refuses the call past limit. A limit of
// zero or less never refuses.
func NewLimiter(limit int) *Limiter {
	return &Limiter{limit: limit}
}

// WithLimiter returns ctx carrying limiter.
func WithLimiter(ctx context.Context, limiter *Limiter) context.Context {
	return context.WithValue(ctx, limiterKey{}, limiter)
}

func (l *Limiter) reserve() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limit > 0 && l.started >= l.limit {
		return spi.NewUserError(
			LimitExceededCode,
			fmt.Sprintf("The execution reached its limit of %d HTTP calls", l.limit),
			nil,
		)
	}
	l.started++
	return nil
}

// Collector gathers the calls of one top-level node. It is safe for
// concurrent use by the parallel nodes of a body.
type Collector struct {
	mu    sync.Mutex
	calls []record
}

type record struct {
	key           groupKey
	startedAt     time.Time
	durationMs    int64
	statusCode    int
	transportErr  spi.HTTPCallErrorClass
	responseBytes *int64
}

type groupKey struct {
	nodeID, method, host, pathTemplate string
}

// NewCollector returns an empty collector.
func NewCollector() *Collector {
	return &Collector{}
}

// WithCollector returns ctx carrying collector.
func WithCollector(ctx context.Context, collector *Collector) context.Context {
	return context.WithValue(ctx, collectorKey{}, collector)
}

// CollectorFrom returns the collector in ctx, or nil.
func CollectorFrom(ctx context.Context) *Collector {
	if ctx == nil {
		return nil
	}
	collector, _ := ctx.Value(collectorKey{}).(*Collector)
	return collector
}

func limiterFrom(ctx context.Context) *Limiter {
	if ctx == nil {
		return nil
	}
	limiter, _ := ctx.Value(limiterKey{}).(*Limiter)
	return limiter
}

// Groups returns the collected calls grouped by node, method, host and path
// template. Groups are ordered by their first call and calls by start time.
func (c *Collector) Groups() []spi.HTTPCallGroup {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	calls := slices.Clone(c.calls)
	c.mu.Unlock()
	if len(calls) == 0 {
		return nil
	}
	slices.SortStableFunc(calls, func(a, b record) int { return a.startedAt.Compare(b.startedAt) })

	index := map[groupKey]int{}
	var groups []spi.HTTPCallGroup
	for _, call := range calls {
		position, seen := index[call.key]
		if !seen {
			position = len(groups)
			index[call.key] = position
			groups = append(groups, spi.HTTPCallGroup{
				NodeID:       call.key.nodeID,
				Method:       call.key.method,
				Host:         call.key.host,
				PathTemplate: call.key.pathTemplate,
				StartedAt:    call.startedAt,
			})
		}
		appendCall(&groups[position], call)
	}
	return groups
}

func appendCall(group *spi.HTTPCallGroup, call record) {
	i := group.Count()
	group.OffsetsMs = append(group.OffsetsMs, call.startedAt.Sub(group.StartedAt).Milliseconds())
	group.DurationsMs = append(group.DurationsMs, call.durationMs)
	group.StatusCodes = append(group.StatusCodes, call.statusCode)
	if call.transportErr != "" {
		if group.TransportErrors == nil {
			group.TransportErrors = map[string]spi.HTTPCallErrorClass{}
		}
		group.TransportErrors[strconv.Itoa(i)] = call.transportErr
	}
	if call.responseBytes != nil {
		// A node either always reads a response size or never does, so the
		// array is either complete or absent.
		group.ResponseBytes = append(group.ResponseBytes, *call.responseBytes)
	}
}

// Target names the call a node is about to send.
type Target struct {
	NodeID string
	Method string
	// URL is the resolved URL the request is sent to.
	URL string
	// URLTemplate is the node's URL before template resolution.
	URLTemplate string
}

// Call is a started call. A nil *Call and a call started without a collector
// record nothing.
type Call struct {
	collector *Collector
	record    record
}

// Start reserves the next call of the execution in ctx. Past the limit it
// returns a user error with LimitExceededCode, and the node must not send the
// request.
func Start(ctx context.Context, target Target) (*Call, error) {
	if err := limiterFrom(ctx).reserve(); err != nil {
		return nil, err
	}
	return &Call{
		collector: CollectorFrom(ctx),
		record: record{
			key: groupKey{
				nodeID:       target.NodeID,
				method:       strings.ToUpper(target.Method),
				host:         host(target.URL),
				pathTemplate: PathTemplate(target.URLTemplate),
			},
			startedAt: time.Now().UTC(),
		},
	}, nil
}

// Outcome is how a call ended. StatusCode is 0 when no response arrived, and
// TransportError then says why. Leave ResponseBytes nil for a streamed body.
type Outcome struct {
	StatusCode     int
	TransportError spi.HTTPCallErrorClass
	ResponseBytes  *int64
}

// Finish records the call in the node's collector.
func (c *Call) Finish(outcome Outcome) {
	if c == nil || c.collector == nil {
		return
	}
	call := c.record
	call.durationMs = time.Since(call.startedAt).Milliseconds()
	call.statusCode = outcome.StatusCode
	call.responseBytes = outcome.ResponseBytes
	if outcome.StatusCode == 0 {
		call.transportErr = cmp.Or(outcome.TransportError, spi.HTTPCallErrorConnect)
	}

	c.collector.mu.Lock()
	c.collector.calls = append(c.collector.calls, call)
	c.collector.mu.Unlock()
}

func host(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}
