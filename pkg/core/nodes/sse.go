package nodes

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const (
	defaultSseMaxEvents = 100
	defaultSseTimeout   = 30 * time.Second
	sseMaxLineBytes     = 1024 * 1024
)

// SseCfg configures a Server-Sent-Events node: connect to a text/event-stream,
// parse events, and run the node's assertions per event until a stop condition.
// Self-evaluating (Assert:None) — it owns its per-event assertion cadence.
type SseCfg struct {
	node.Base

	Method                 string            `json:"method"`
	URL                    string            `json:"url"`
	Headers                map[string]string `json:"headers"`
	MaxEvents              int               `json:"max_events"`
	TimeoutMs              int64             `json:"timeout_ms"`
	CompletionEvent        string            `json:"completion_event"`
	StopOnAssertionFailure *bool             `json:"stop_on_assertion_failure"`
}

func runSse(ctx context.Context, cfg SseCfg, _ value.Value, rt node.Runtime) (node.Result, error) {
	timeout := defaultSseTimeout
	if cfg.TimeoutMs > 0 {
		timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := sseConnect(ctx, cfg, rt)
	if err != nil {
		return node.Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), sseMaxLineBytes)

	events, stopReason, err := readSseStream(scanner, cfg)
	if err != nil {
		if stopReason == "assertion_failure" {
			return node.Result{}, err
		}
		if ctx.Err() != context.DeadlineExceeded {
			return node.Result{}, node.UserErrf("SSE_FAILED", "sse read: %v", err)
		}
		stopReason = "timeout"
	} else if ctx.Err() == context.DeadlineExceeded {
		stopReason = "timeout"
	}

	var last any
	if len(events) > 0 {
		last = events[len(events)-1]
	}
	return node.Result{Outputs: value.Map{
		"events":      value.Of(events),
		outKeyCount:   value.Of(len(events)),
		"last":        value.Of(last),
		"stop_reason": value.Of(stopReason),
	}}, nil
}

// sseParser accumulates field lines into events per the SSE grammar.
type sseParser struct {
	dataLines []string
	eventName string
}

// feed consumes one line; when it completes an event (a blank line after data),
// it returns the joined data, the event name, and complete=true.
func (p *sseParser) feed(line string) (string, string, bool) {
	if strings.HasPrefix(line, ":") {
		return "", "", false // comment
	}
	if line != "" {
		if field, val := splitSseField(line); field == "data" {
			p.dataLines = append(p.dataLines, val)
		} else if field == "event" {
			p.eventName = val
		}
		return "", "", false
	}
	if len(p.dataLines) == 0 {
		p.eventName = ""
		return "", "", false
	}
	data, name := strings.Join(p.dataLines, "\n"), p.eventName
	p.dataLines, p.eventName = nil, ""
	return data, name, true
}

// readSseStream parses events until a stop condition. A non-nil error with
// stopReason "assertion_failure" is a node failure; any other non-nil error is
// the scanner's (e.g. a cancelled read).
func readSseStream(scanner *bufio.Scanner, cfg SseCfg) ([]any, string, error) {
	maxEvents := cfg.MaxEvents
	if maxEvents <= 0 {
		maxEvents = defaultSseMaxEvents
	}
	stopOnFail := cfg.StopOnAssertionFailure == nil || *cfg.StopOnAssertionFailure

	var p sseParser
	events := []any{}
	for scanner.Scan() {
		data, name, complete := p.feed(scanner.Text())
		if !complete {
			continue
		}
		parsed := parseSseData(data)
		events = append(events, parsed)
		if stop, reason, err := evalSseEvent(cfg, parsed, data, name, len(events), maxEvents, stopOnFail); stop {
			return events, reason, err
		}
	}
	return events, "eof", scanner.Err()
}

// evalSseEvent applies the stop conditions to a freshly dispatched event.
func evalSseEvent(
	cfg SseCfg,
	parsed any,
	data, name string,
	count, maxEvents int,
	stopOnFail bool,
) (bool, string, error) {
	if len(cfg.Assertions) > 0 && !assert.Run(value.Of(parsed), cfg.Assertions).AllPassed() && stopOnFail {
		return true, "assertion_failure",
			node.UserErrf("SSE_FAILED", "sse assertion failed at event %d", count-1)
	}
	if cfg.CompletionEvent != "" && (name == cfg.CompletionEvent || data == cfg.CompletionEvent) {
		return true, "completion_event", nil
	}
	if count >= maxEvents {
		return true, "max_events", nil
	}
	return false, "", nil
}

func sseConnect(ctx context.Context, cfg SseCfg, rt node.Runtime) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, cmp.Or(cfg.Method, http.MethodGet), cfg.URL, nil)
	if err != nil {
		return nil, node.UserErrf("SSE_FAILED", "sse build request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := rt.HTTP.Do(req)
	if err != nil {
		return nil, node.UserErrf("SSE_FAILED", "sse connect: %v", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, node.UserErrf("SSE_FAILED", "sse non-2xx status %d", resp.StatusCode)
	}
	return resp, nil
}

// parseSseData parses an event's data as JSON, falling back to the raw string.
func parseSseData(data string) any {
	var v any
	if err := json.Unmarshal([]byte(data), &v); err == nil {
		return v
	}
	return data
}

// splitSseField splits "field: value", stripping one optional leading space.
func splitSseField(line string) (string, string) {
	field, val, found := strings.Cut(line, ":")
	if !found {
		return line, ""
	}
	return field, strings.TrimPrefix(val, " ")
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindSse, runSse) }
