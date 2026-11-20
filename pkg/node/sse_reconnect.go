package node

import (
	"context"
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// sseConnectAttempts is how many connections an SSE node opens at most when the
// transport fails before a stop condition: the first and two reconnects. They
// share the node's timeout_ms and wait requestTransportBackoff, then twice it.
const sseConnectAttempts = 3

// How a reconnect treats the events the cut connection delivered.
const (
	// sseReconnectResume keeps them and sends Last-Event-ID: the stream gave
	// event ids, so a repeated id is recognised and dropped.
	sseReconnectResume = "resume"
	// sseReconnectRestart discards them: without ids a replay cannot be told
	// from new events, so the node collects the new connection's events only.
	sseReconnectRestart = "restart"
)

// sseStream is one SSE node execution: up to sseConnectAttempts connections
// feeding one parser.
type sseStream struct {
	node        *SseNode
	method      string
	url         string
	headers     map[string]string
	parser      *sseParser
	connections int
}

func (s *sseStream) exchange(stopReason string) sseExchange {
	return sseExchange{
		Method: s.method, URL: s.url, Events: s.parser.events,
		AssertionResults: s.parser.assertionResults, StopReason: stopReason,
	}
}

// run connects and consumes until a stop condition, a failure, or ctx's
// deadline. A connection that fails before any response, or a stream the
// transport cuts before a stop condition, is reopened when
// retryableTransportError allows it for the node's method and headers.
// Statuses, assertion failures, a clean end of stream and the deadline never
// reconnect.
func (s *sseStream) run(ctx context.Context) (sseExchange, error) {
	client := recordingClient(s.node.GetID(), s.node.Data.URL, true)
	// cutErr is the read error that ended the previous connection, if any.
	var cutErr error
	for attempt := 1; ; attempt++ {
		s.connections = attempt
		req, err := s.newRequest(ctx)
		if err != nil {
			return s.exchange(""), err
		}

		// No client-level timeout: streaming is bounded by ctx instead, so a
		// long-lived stream is not aborted mid-read by an http.Client deadline.
		resp, err := client.Do(req)
		if err != nil {
			retry, exchange, failure := s.connectFailed(ctx, req, err, attempt, cutErr)
			if retry {
				continue
			}
			return exchange, failure
		}

		stopReason, assertErr, readErr := s.consume(ctx, resp)
		if assertErr != nil {
			return s.exchange(stopReason), assertErr
		}
		if readErr == nil {
			return s.exchange(stopReason), nil
		}

		// The target closed or reset the stream; the cause stays off the log line.
		cutErr = spi.NewUserError("SSE_FAILED", "SSE stream read error", readErr)
		if !s.reconnectAfterCut(ctx, req, readErr, attempt) {
			return s.exchange(sseStopEOF), cutErr
		}
	}
}

// connectFailed handles a connection that failed before any response. retry
// reports that the next attempt may start; otherwise the exchange and error
// end the node.
func (s *sseStream) connectFailed(
	ctx context.Context, req *http.Request, err error, attempt int, cutErr error,
) (bool, sseExchange, error) {
	if userErr, ok := spi.AsUserError(err); ok {
		return false, s.exchange(""), userErr
	}
	// The deadline can elapse during the connect/header phase (a slow producer
	// that has not yet sent the status line). That is the configured
	// timeout_ms stop condition, not a node failure.
	if contextDone(ctx) {
		exchange, failure := s.outOfTime(cutErr)
		return false, exchange, failure
	}
	if attempt >= sseConnectAttempts ||
		!retryableTransportError(s.method, req.Header, err) ||
		waitBeforeRetry(ctx, attempt) != nil {
		// A deadline that elapses during the backoff leaves this failure as
		// the outcome, not a clean timeout: no stream was read.
		if cutErr != nil {
			return false, s.exchange(sseStopEOF), cutErr
		}
		return false, s.exchange(""), classifyRequestError(s.url, err)
	}
	s.logReconnect(attempt, "connect", "")
	return true, sseExchange{}, nil
}

// reconnectAfterCut reports whether a stream the transport cut reopens, and
// readies the parser for it after the backoff.
func (s *sseStream) reconnectAfterCut(ctx context.Context, req *http.Request, readErr error, attempt int) bool {
	if attempt >= sseConnectAttempts ||
		!retryableTransportError(s.method, req.Header, readErr) ||
		!s.parser.canReconnect() ||
		waitBeforeRetry(ctx, attempt) != nil {
		return false
	}
	s.logReconnect(attempt, "read", s.parser.prepareReconnect())
	return true
}

// outOfTime ends a stream whose deadline elapsed while it was connecting: a
// clean timeout when no connection was cut, else the cut that started the
// reconnect.
func (s *sseStream) outOfTime(cutErr error) (sseExchange, error) {
	if cutErr != nil {
		return s.exchange(sseStopEOF), cutErr
	}
	return s.exchange(sseStopTimeout), nil
}

func (s *sseStream) newRequest(ctx context.Context) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, s.method, s.url, nil)
	if err != nil {
		return nil, spi.NewUserError(
			"SSE_FAILED", fmt.Sprintf("Could not build the SSE request for %s", requestHost(s.url)), err,
		)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for key, value := range s.headers {
		req.Header.Set(key, value)
	}
	// Set only once the stream committed an id, so a configured Last-Event-ID
	// still opens the first connection.
	if s.parser.lastEventID != "" {
		req.Header.Set("Last-Event-ID", s.parser.lastEventID)
	}
	return req, nil
}

// consume reads one connection's response. A non-2xx status is a failure that
// never reconnects.
func (s *sseStream) consume(ctx context.Context, resp *http.Response) (string, error, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", spi.NewUserError(
			"SSE_FAILED", fmt.Sprintf("SSE endpoint returned non-2xx status: %d", resp.StatusCode), nil,
		), nil
	}
	return s.node.consume(ctx, resp.Body, s.parser)
}

func (s *sseStream) logReconnect(attempt int, phase, mode string) {
	event := log.Info().
		Str("nodeID", s.node.GetID()).
		Int("attempt", attempt+1).
		Str("phase", phase)
	if mode != "" {
		event = event.Str("mode", mode)
	}
	event.Msg("SSE node reconnecting after a transport failure")
}

// resumable reports whether the next connection can resume: every collected
// event carried its own id, so a replay is recognised, and the last one was
// committed.
func (p *sseParser) resumable() bool {
	return p.lastEventID != "" && !p.idlessEvents
}

// canReconnect reports whether a cut stream may reopen without hiding an
// outcome: a restart would erase a failing event assertion the node survived.
func (p *sseParser) canReconnect() bool {
	return p.resumable() || !p.assertionFailed
}

// prepareReconnect readies the parser for the next connection and returns its
// mode. When resumable, the collected events stay and repeated ids are
// dropped; otherwise the collected events and their assertion results are
// discarded and no Last-Event-ID is sent, so a server that replays its stream
// (as Echopoint's execution stream does) is not counted twice.
func (p *sseParser) prepareReconnect() string {
	if p.resumable() {
		p.skipSeen = true
		return sseReconnectResume
	}
	p.events = nil
	p.assertionResults = nil
	p.seenIDs = map[string]struct{}{}
	p.skipSeen = false
	p.idlessEvents = false
	p.idBuffer, p.lastEventID = "", ""
	return sseReconnectRestart
}
