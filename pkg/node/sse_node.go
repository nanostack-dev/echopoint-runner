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
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
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
	dynamic spi.DynamicResolver
}

// sseExchange names the stream state shared by success and failure results.
// It stays private; SseExecutionResult owns the serialized consumer contract.
type sseExchange struct {
	Method           string
	URL              string
	Events           []any
	AssertionResults []spi.AssertionResult
	StopReason       string
}

// AsSseNode safely casts an AnyNode to an SseNode.
// Returns the SseNode and true if the cast succeeds, nil and false otherwise.
func AsSseNode(node AnyNode) (*SseNode, bool) {
	sseNode, ok := node.(*SseNode)
	return sseNode, ok
}

func (n *SseNode) GetData() SseData {
	return n.Data
}

// InputSchema infers inputs from template variables in URL, Headers and
// assertion expected values.
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
	collect(AssertionReferences(n))

	result := make([]string, 0, len(vars))
	for v := range vars {
		result = append(result, v)
	}
	return result
}

// OutputSchema returns the keys this node always produces.
func (n *SseNode) OutputSchema() []string {
	return []string{"events", outputKeyCount, "last"}
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
func (n *SseNode) Execute(ctx spi.ExecutionContext) (spi.AnyResult, error) {
	startTime := time.Now()
	n.dynamic = ctx.DynamicVars

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("inputCount", len(ctx.Inputs)).
		Msg("Starting SSE node execution")

	method := strings.ToUpper(strings.TrimSpace(n.Data.Method))
	if method == "" {
		method = http.MethodGet
	}

	url, headers, err := n.prepareConnection(ctx.Inputs)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, sseExchange{Method: method, URL: n.Data.URL}, err, startTime), err
	}
	assertions, err := ResolveAssertions(ctx, n.GetAssertions())
	if err != nil {
		return n.createErrorResult(ctx.Inputs, sseExchange{Method: method, URL: url}, err, startTime), err
	}

	timeout := time.Duration(n.timeoutMs()) * time.Millisecond
	streamCtx, cancel := context.WithTimeout(ctx.Context(), timeout)
	defer cancel()

	stream := sseStream{
		node: n, method: method, url: url, headers: headers,
		parser: &sseParser{node: n, assertions: assertions, seenIDs: map[string]struct{}{}},
	}
	exchange, err := stream.run(streamCtx)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, exchange, err, startTime), err
	}

	result := n.createSuccessResult(ctx.Inputs, exchange, startTime)
	log.Info().
		Str("nodeID", n.GetID()).
		Int("eventCount", len(exchange.Events)).
		Str("stopReason", exchange.StopReason).
		Int("connections", stream.connections).
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
			headers[key] = FormatTemplateValue(resolved)
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
	node *SseNode
	// assertions are the node's assertions with their expected values resolved.
	assertions       []CompositeAssertion
	dataLines        []string
	eventName        string
	haveData         bool
	events           []any
	assertionResults []spi.AssertionResult
	// eventID is the explicit id field of the event being buffered, if any.
	eventID    string
	hasEventID bool
	// idBuffer is the spec's last event ID buffer: set by an id field, it
	// survives id-less events. lastEventID is the id committed at the last
	// dispatch, sent as Last-Event-ID when the stream resumes; an id whose
	// event the cut left unfinished is never committed.
	idBuffer    string
	lastEventID string
	// idlessEvents records a collected event without its own id: a resume
	// could not recognise its replay, so the stream restarts instead.
	idlessEvents bool
	// seenIDs holds the explicit ids of every collected event; skipSeen drops a
	// repeated id on a resumed connection whose server ignored Last-Event-ID.
	seenIDs  map[string]struct{}
	skipSeen bool
	// assertionFailed records a failing event assertion the stream survived
	// (stop_on_assertion_failure off); a restart would erase it.
	assertionFailed bool
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
	case "id":
		// Per the spec, an id containing NULL is ignored and an empty id resets
		// the last event ID.
		if !strings.ContainsRune(value, 0) {
			p.eventID, p.hasEventID = value, true
			p.idBuffer = value
		}
	case "retry":
		// The reconnect delay is the runner's own bounded backoff.
	default:
		// Unknown field — ignore per the SSE spec.
	}
	return "", nil
}

// dispatch finalizes the buffered event, runs its assertions, and reports any
// stop condition triggered by it.
func (p *sseParser) dispatch() (string, error) {
	// The spec commits the last event ID at every dispatch, even of an
	// event that carries no data.
	p.lastEventID = p.idBuffer
	if !p.haveData && p.eventName == "" {
		p.eventID, p.hasEventID = "", false
		return "", nil
	}
	ev := sseEvent{name: p.eventName, data: strings.Join(p.dataLines, "\n")}
	eventID, hasEventID := p.eventID, p.hasEventID && p.eventID != ""
	p.clearBuffer()

	if hasEventID {
		if _, seen := p.seenIDs[eventID]; seen && p.skipSeen {
			// A replay of an event this node already collected.
			return "", nil
		}
		p.seenIDs[eventID] = struct{}{}
	} else {
		p.idlessEvents = true
	}

	parsed := parseSseData(ev.data)
	p.events = append(p.events, parsed)
	eventIndex := len(p.events) - 1

	results, assertErr := p.node.evaluateEventAssertions(p.assertions, parsed, eventIndex)
	p.assertionResults = append(p.assertionResults, results...)
	if assertErr != nil {
		if p.node.stopOnAssertionFailure() {
			return sseStopAssertionFailure, assertErr
		}
		p.assertionFailed = true
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

// clearBuffer drops the event being buffered. Every connection ends with it,
// so an id line of an event the cut left unfinished is forgotten too.
func (p *sseParser) clearBuffer() {
	p.dataLines = nil
	p.eventName = ""
	p.haveData = false
	p.eventID, p.hasEventID = "", false
	p.idBuffer = p.lastEventID
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

// consume stream-parses one connection's body into p, dispatching events and
// running per-event assertions until a stop condition is met. It returns the
// stop reason, a failing assertion under stop_on_assertion_failure, and the raw
// error that ended the read when the target closed or reset the stream. An
// event left half-received by a cut connection is dropped, as the spec says.
func (n *SseNode) consume(ctx context.Context, body io.Reader, p *sseParser) (string, error, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, sseScannerInitBytes), sseScannerBufferBytes)
	defer p.clearBuffer()
	if p.events == nil {
		p.events = make([]any, 0)
	}

	for scanner.Scan() {
		// Honor cancellation/deadline between lines so a slow producer cannot
		// keep us reading past the overall timeout. A done context is the
		// configured timeout_ms stop condition, not a node failure.
		if contextDone(ctx) {
			return sseStopTimeout, nil, nil
		}

		line := strings.TrimRight(scanner.Text(), "\r")
		stopReason, err := p.feed(line)
		if err != nil {
			return stopReason, err, nil
		}
		if stopReason != "" {
			return stopReason, nil, nil
		}
	}

	// A context cancellation surfaces either as a done context or as a scanner
	// read error; in both cases treat it as a clean timeout-bounded stop rather
	// than a node failure.
	if contextDone(ctx) {
		return sseStopTimeout, nil, nil
	}
	return sseStopEOF, nil, scanner.Err()
}

// evaluateEventAssertions runs every node assertion against a single event's
// parsed data via the shared EvaluateAssertions seam, then repurposes each
// result's Index to carry the event index (instead of the per-assertion index)
// so callers can attribute the assertion to the event that produced it.
// Evaluation stops at the first failing/erroring assertion (which IS recorded)
// and the error is returned.
func (n *SseNode) evaluateEventAssertions(
	assertions []CompositeAssertion, parsed any, eventIndex int,
) ([]spi.AssertionResult, error) {
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
		// A failing event assertion is a user-caused outcome, and one the node
		// often survives: with stop_on_assertion_failure off the stream keeps
		// going and the node still succeeds, so this fires per failing event and
		// at error level would be pure alert noise. Classify like the engine's
		// node-failure log — UserError at debug, anything else a real fault.
		event := log.Error()
		if _, ok := spi.AsUserError(err); ok {
			event = log.Debug()
		}
		event.
			Str("nodeID", n.GetID()).
			Int("eventIndex", eventIndex).
			Err(err).
			Msg("SSE event assertion failed")
		// %w keeps the UserError reachable by errors.As, so when this does stop
		// the stream the engine still classifies the node failure at debug.
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
	exchange sseExchange,
	startTime time.Time,
) *SseExecutionResult {
	var last any
	if len(exchange.Events) > 0 {
		last = exchange.Events[len(exchange.Events)-1]
	}
	outputs := map[string]any{
		"events":       exchange.Events,
		outputKeyCount: len(exchange.Events),
		"last":         last,
	}

	return &SseExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:           n.GetID(),
			DisplayName:      n.GetDisplayName(),
			NodeType:         spi.KindSse,
			Inputs:           inputs,
			Outputs:          outputs,
			ExecutedAt:       time.Now(),
			AssertionResults: exchange.AssertionResults,
		},
		RequestMethod: exchange.Method,
		RequestURL:    exchange.URL,
		Events:        exchange.Events,
		EventCount:    len(exchange.Events),
		StopReason:    exchange.StopReason,
		DurationMs:    time.Since(startTime).Milliseconds(),
	}
}

func (n *SseNode) createErrorResult(
	inputs map[string]any,
	exchange sseExchange,
	err error,
	startTime time.Time,
) spi.AnyResult {
	errMsg := err.Error()
	errCode := "SSE_FAILED"

	base := failedNodeBase(n.BaseNode, inputs, err, failureDetails{Kind: spi.KindSse, Code: errCode, Message: errMsg})
	base.AssertionResults = exchange.AssertionResults

	return &SseExecutionResult{
		BaseExecutionResult: base,
		RequestMethod:       exchange.Method,
		RequestURL:          exchange.URL,
		Events:              exchange.Events,
		EventCount:          len(exchange.Events),
		StopReason:          exchange.StopReason,
		DurationMs:          time.Since(startTime).Milliseconds(),
	}
}
