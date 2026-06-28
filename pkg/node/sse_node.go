package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
)

const (
	// defaultSseMaxEvents caps how many events the node collects before stopping
	// when the node leaves max_events unset.
	defaultSseMaxEvents = 100
	// defaultSseTimeoutMs bounds the overall streaming lifetime when the node
	// leaves timeout_ms unset.
	defaultSseTimeoutMs = 30000
	// sseScannerInitBytes is the scanner's initial line-buffer capacity.
	sseScannerInitBytes = 64 * 1024
	// sseScannerBufferBytes is a generous per-line buffer cap so large SSE data
	// frames (e.g. embedded JSON payloads) are not truncated mid-stream.
	sseScannerBufferBytes = 1024 * 1024
)

// SSE stop reasons, recorded on the result so callers can tell why streaming
// ended without re-deriving it.
const (
	sseStopMaxEvents        = "max_events"
	sseStopCompletionEvent  = "completion_event"
	sseStopTimeout          = "timeout"
	sseStopEOF              = "eof"
	sseStopAssertionFailure = "assertion_failure"
)

// SseData configures a connection to a text/event-stream endpoint that is
// consumed event-by-event over time.
type SseData struct {
	// URL is the event-stream endpoint (templated).
	URL string `json:"url"`
	// Method defaults to GET when empty.
	Method string `json:"method"`
	// Headers are sent on the request (values templated).
	Headers map[string]string `json:"headers"`
	// MaxEvents stops the stream after N events (default defaultSseMaxEvents).
	MaxEvents int `json:"max_events"`
	// TimeoutMs is the overall deadline in milliseconds (default defaultSseTimeoutMs).
	TimeoutMs int `json:"timeout_ms"`
	// CompletionEvent, when set, stops the stream as soon as an event whose
	// "event:" name OR raw data equals this value is dispatched.
	CompletionEvent string `json:"completion_event"`
	// StopOnAssertionFailure stops (and fails) the node on the first failing
	// per-event assertion. Defaults to true.
	StopOnAssertionFailure *bool `json:"stop_on_assertion_failure"`
}

// SseNode connects to a Server-Sent Events endpoint, consumes events over time,
// and runs the node's assertions against each event's parsed data with a
// cross-event accumulator. Unlike the buffered single-shot request node, it
// streams: events are processed as they arrive and the connection is closed as
// soon as a stop condition (max_events, completion_event, timeout, assertion
// failure, or EOF) is met.
type SseNode struct {
	BaseNode

	Data SseData `json:"data"`

	// dynamic resolves {{$name}} variables; set per execution, not serialized.
	dynamic DynamicResolver
}

// AsSseNode safely casts an AnyNode to an SseNode.
// Returns the SseNode and true if the cast succeeds, nil and false otherwise.
func AsSseNode(node AnyNode) (*SseNode, bool) {
	sseNode, ok := node.(*SseNode)
	return sseNode, ok
}

// MustAsSseNode casts an AnyNode to an SseNode, panicking if it fails.
// Use this when you're certain the node is an SseNode.
func MustAsSseNode(node AnyNode) *SseNode {
	sseNode, ok := AsSseNode(node)
	if !ok {
		panic("expected SseNode but got different type")
	}
	return sseNode
}

func (n *SseNode) GetData() SseData {
	return n.Data
}

// InputSchema infers inputs from template variables in URL and Headers.
func (n *SseNode) InputSchema() []string {
	si := &SchemaInference{}
	vars := make(map[string]bool)
	collect := func(found []string) {
		for _, v := range found {
			vars[v] = true
		}
	}
	collect(si.ExtractTemplateVariables(n.Data.URL))
	collect(si.ExtractTemplateVariables(n.Data.Headers))

	result := make([]string, 0, len(vars))
	for v := range vars {
		result = append(result, v)
	}
	return result
}

// OutputSchema returns the keys this node always produces.
func (n *SseNode) OutputSchema() []string {
	return []string{"events", "count", "last"}
}

func (n *SseNode) maxEvents() int {
	if n.Data.MaxEvents > 0 {
		return n.Data.MaxEvents
	}
	return defaultSseMaxEvents
}

func (n *SseNode) timeoutMs() int {
	if n.Data.TimeoutMs > 0 {
		return n.Data.TimeoutMs
	}
	return defaultSseTimeoutMs
}

func (n *SseNode) stopOnAssertionFailure() bool {
	if n.Data.StopOnAssertionFailure == nil {
		return true
	}
	return *n.Data.StopOnAssertionFailure
}

// Execute connects to the SSE endpoint and consumes events until a stop
// condition is met. On any failure it returns a populated result plus the error
// so the engine records a failed node.
func (n *SseNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()
	n.dynamic = ctx.DynamicVars

	log.Debug().
		Str("nodeID", n.GetID()).
		Any("inputs", ctx.Inputs).
		Msg("Starting SSE node execution")

	method := strings.ToUpper(strings.TrimSpace(n.Data.Method))
	if method == "" {
		method = http.MethodGet
	}

	url, headers, err := n.prepareConnection(ctx.Inputs)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, method, n.Data.URL, nil, nil, "", err, startTime), err
	}

	timeout := time.Duration(n.timeoutMs()) * time.Millisecond
	streamCtx, cancel := context.WithTimeout(ctx.Context(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(streamCtx, method, url, nil)
	if err != nil {
		wrapped := fmt.Errorf("failed to build SSE request: %w", err)
		return n.createErrorResult(ctx.Inputs, method, url, nil, nil, "", wrapped, startTime), wrapped
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// No client-level timeout: streaming is bounded by streamCtx instead, so a
	// long-lived stream is not aborted mid-read by an http.Client deadline.
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		// The overall deadline can elapse during the connect/header phase (a slow
		// producer that has not yet sent the status line). That is the configured
		// timeout_ms stop condition, not a node failure — return a clean,
		// empty-event success result so callers see "timeout", not an error.
		if contextDone(streamCtx) {
			result := n.createSuccessResult(ctx.Inputs, method, url, nil, nil, sseStopTimeout, startTime)
			return result, nil
		}
		wrapped := fmt.Errorf("SSE connection failed: %w", err)
		return n.createErrorResult(ctx.Inputs, method, url, nil, nil, "", wrapped, startTime), wrapped
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr := fmt.Errorf("SSE endpoint returned non-2xx status: %d", resp.StatusCode)
		return n.createErrorResult(ctx.Inputs, method, url, nil, nil, "", statusErr, startTime), statusErr
	}

	events, assertionResults, stopReason, streamErr := n.consume(streamCtx, resp.Body)
	if streamErr != nil {
		return n.createErrorResult(
			ctx.Inputs, method, url, events, assertionResults, stopReason, streamErr, startTime,
		), streamErr
	}

	result := n.createSuccessResult(ctx.Inputs, method, url, events, assertionResults, stopReason, startTime)
	log.Info().
		Str("nodeID", n.GetID()).
		Int("eventCount", len(events)).
		Str("stopReason", stopReason).
		Int64("durationMs", result.DurationMs).
		Msg("SSE node executed successfully")
	return result, nil
}

// prepareConnection resolves the templated URL and headers.
func (n *SseNode) prepareConnection(inputs map[string]any) (string, map[string]string, error) {
	resolver := NewTemplateResolverWithDynamics(inputs, n.dynamic)

	resolvedURL, err := resolver.Resolve(n.Data.URL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to resolve SSE URL templates: %w", err)
	}
	url, ok := resolvedURL.(string)
	if !ok {
		return "", nil, fmt.Errorf("resolved SSE URL is not a string: %T", resolvedURL)
	}

	headers := make(map[string]string, len(n.Data.Headers))
	for key, value := range n.Data.Headers {
		resolved, resolveErr := resolver.Resolve(value)
		if resolveErr != nil {
			return "", nil, fmt.Errorf("failed to resolve SSE header %q: %w", key, resolveErr)
		}
		if s, isString := resolved.(string); isString {
			headers[key] = s
		} else {
			headers[key] = fmt.Sprintf("%v", resolved)
		}
	}

	return url, headers, nil
}

// sseEvent is a single dispatched Server-Sent Event.
type sseEvent struct {
	name string
	data string
}

// sseParser holds the cross-line parse state plus the cross-event accumulator
// for a single SSE consumption.
type sseParser struct {
	node             *SseNode
	dataLines        []string
	eventName        string
	haveData         bool
	events           []any
	assertionResults []AssertionResult
}

// feed processes one stream line. It returns a stop reason (empty when the
// stream should continue) and an error (set only on assertion failure under
// stop_on_assertion_failure).
func (p *sseParser) feed(line string) (string, error) {
	// A blank line dispatches the buffered event.
	if line == "" {
		return p.dispatch()
	}
	// A line that starts with ":" is a comment per the SSE spec.
	if strings.HasPrefix(line, ":") {
		return "", nil
	}

	field, value := splitSseField(line)
	switch field {
	case "data":
		p.dataLines = append(p.dataLines, value)
		p.haveData = true
	case "event":
		p.eventName = value
	case "id", "retry":
		// Recorded by the spec but not needed for assertion/accumulation.
	default:
		// Unknown field — ignore per the SSE spec.
	}
	return "", nil
}

// dispatch finalizes the buffered event, runs its assertions, and reports any
// stop condition triggered by it.
func (p *sseParser) dispatch() (string, error) {
	if !p.haveData && p.eventName == "" {
		return "", nil
	}
	ev := sseEvent{name: p.eventName, data: strings.Join(p.dataLines, "\n")}
	p.dataLines = nil
	p.eventName = ""
	p.haveData = false

	parsed := parseSseData(ev.data)
	p.events = append(p.events, parsed)
	eventIndex := len(p.events) - 1

	results, assertErr := p.node.evaluateEventAssertions(parsed, eventIndex)
	p.assertionResults = append(p.assertionResults, results...)
	if assertErr != nil && p.node.stopOnAssertionFailure() {
		return sseStopAssertionFailure, assertErr
	}

	if p.node.Data.CompletionEvent != "" &&
		(ev.name == p.node.Data.CompletionEvent || ev.data == p.node.Data.CompletionEvent) {
		return sseStopCompletionEvent, nil
	}
	if len(p.events) >= p.node.maxEvents() {
		return sseStopMaxEvents, nil
	}
	return "", nil
}

// contextDone reports whether ctx has been cancelled or has hit its deadline.
// It deliberately discards ctx.Err() because, for SSE consumption, a done
// context is the configured timeout_ms stop condition rather than a failure to
// propagate.
func contextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// consume stream-parses the SSE body, dispatching events and running per-event
// assertions until a stop condition is met. It returns the collected event data
// (parsed), the accumulated assertion results (Index = event index), the stop
// reason, and a non-nil error only when streaming failed (read error or an
// assertion failure with stop_on_assertion_failure).
func (n *SseNode) consume(
	ctx context.Context, body io.Reader,
) ([]any, []AssertionResult, string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, sseScannerInitBytes), sseScannerBufferBytes)

	p := &sseParser{node: n, events: make([]any, 0, n.maxEvents())}

	for scanner.Scan() {
		// Honor cancellation/deadline between lines so a slow producer cannot
		// keep us reading past the overall timeout. A done context is the
		// configured timeout_ms stop condition, not a node failure.
		if contextDone(ctx) {
			return p.events, p.assertionResults, sseStopTimeout, nil
		}

		line := strings.TrimRight(scanner.Text(), "\r")
		stopReason, err := p.feed(line)
		if err != nil {
			return p.events, p.assertionResults, stopReason, err
		}
		if stopReason != "" {
			return p.events, p.assertionResults, stopReason, nil
		}
	}

	// A context cancellation surfaces either as a done context or as a scanner
	// read error; in both cases treat it as a clean timeout-bounded stop rather
	// than a node failure.
	if contextDone(ctx) {
		return p.events, p.assertionResults, sseStopTimeout, nil
	}
	if err := scanner.Err(); err != nil {
		readErr := fmt.Errorf("SSE stream read error: %w", err)
		return p.events, p.assertionResults, sseStopEOF, readErr
	}
	return p.events, p.assertionResults, sseStopEOF, nil
}

// evaluateEventAssertions runs every node assertion against a single event's
// parsed data via the shared EvaluateAssertions seam, then repurposes each
// result's Index to carry the event index (instead of the per-assertion index)
// so callers can attribute the assertion to the event that produced it.
// Evaluation stops at the first failing/erroring assertion (which IS recorded)
// and the error is returned.
func (n *SseNode) evaluateEventAssertions(parsed any, eventIndex int) ([]AssertionResult, error) {
	assertions := n.GetAssertions()
	if len(assertions) == 0 {
		return nil, nil
	}

	rc := extractors.NewValueResponseContext(parsed)
	results, err := EvaluateAssertions(assertions, rc)
	// EvaluateAssertions assigns Index = assertion index; for SSE the Index slot
	// instead carries the event index so per-event results stay attributable.
	for i := range results {
		results[i].Index = eventIndex
	}
	if err != nil {
		log.Error().
			Str("nodeID", n.GetID()).
			Int("eventIndex", eventIndex).
			Err(err).
			Msg("SSE event assertion failed")
		return results, fmt.Errorf("event %d %w", eventIndex, err)
	}
	return results, nil
}

// parseSseData parses an event's data payload as JSON, falling back to the raw
// string when it is not valid JSON.
func parseSseData(data string) any {
	if data == "" {
		return ""
	}
	var parsed any
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return data
	}
	return parsed
}

// splitSseField splits an SSE field line into its field name and value. Per the
// spec, the value has a single leading space stripped after the colon. A line
// with no colon is a field whose value is the empty string.
func splitSseField(line string) (string, string) {
	field, value, found := strings.Cut(line, ":")
	if !found {
		return line, ""
	}
	return field, strings.TrimPrefix(value, " ")
}

func (n *SseNode) createSuccessResult(
	inputs map[string]any,
	method, url string,
	events []any,
	assertionResults []AssertionResult,
	stopReason string,
	startTime time.Time,
) *SseExecutionResult {
	var last any
	if len(events) > 0 {
		last = events[len(events)-1]
	}
	outputs := map[string]any{
		"events": events,
		"count":  len(events),
		"last":   last,
	}

	return &SseExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:           n.GetID(),
			DisplayName:      n.GetDisplayName(),
			NodeType:         TypeSse,
			Inputs:           inputs,
			Outputs:          outputs,
			ExecutedAt:       time.Now(),
			AssertionResults: assertionResults,
		},
		RequestMethod: method,
		RequestURL:    url,
		Events:        events,
		EventCount:    len(events),
		StopReason:    stopReason,
		DurationMs:    time.Since(startTime).Milliseconds(),
	}
}

func (n *SseNode) createErrorResult(
	inputs map[string]any,
	method, url string,
	events []any,
	assertionResults []AssertionResult,
	stopReason string,
	err error,
	startTime time.Time,
) AnyExecutionResult {
	errMsg := err.Error()
	errCode := "SSE_FAILED"

	log.Error().
		Str("nodeID", n.GetID()).
		Str("url", url).
		Int("eventCount", len(events)).
		Err(err).
		Msg("SSE node execution failed")

	return &SseExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:           n.GetID(),
			DisplayName:      n.GetDisplayName(),
			NodeType:         TypeSse,
			Inputs:           inputs,
			Outputs:          nil,
			Error:            err,
			ErrorMsg:         &errMsg,
			ErrorCode:        &errCode,
			ExecutedAt:       time.Now(),
			AssertionResults: assertionResults,
		},
		RequestMethod: method,
		RequestURL:    url,
		Events:        events,
		EventCount:    len(events),
		StopReason:    stopReason,
		DurationMs:    time.Since(startTime).Milliseconds(),
	}
}
