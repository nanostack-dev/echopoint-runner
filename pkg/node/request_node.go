package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// defaultRequestTimeoutMs is applied when a request node has no explicit timeout
// (Timeout <= 0), avoiding an instant 0ms context deadline.
const defaultRequestTimeoutMs = 30000

type RequestData struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	QueryParams map[string]any    `json:"query_params"`
	Body        any               `json:"body"`
	Timeout     int               `json:"timeout"`
}

// requestExchange carries one request and its completed response through the
// private execution pipeline. It is not a serialized result or public API.
type requestExchange struct {
	URL      string
	Headers  map[string]string
	Body     any
	Response *http.Response
	// ResponseBody is the response body the node kept: the whole body, or its
	// first bytes when no assertion or output reads it (see keptResponseBytes).
	ResponseBody []byte
	// ResponseSize is the whole body's size; zero means len(ResponseBody).
	ResponseSize int64
	ParsedBody   any
	// bodyHold is the response budget reservation holding ResponseBody.
	bodyHold *bodyReservation
}

func (e requestExchange) responseSize() int64 {
	return max(e.ResponseSize, int64(len(e.ResponseBody)))
}

// RequestNode is a typed node for HTTP requests.
type RequestNode struct {
	BaseNode

	Data RequestData `json:"data"`

	// dynamic resolves {{$name}} variables; set per execution, not serialized.
	dynamic spi.DynamicResolver
}

// AsRequestNode safely casts an AnyNode to a RequestNode
// Returns the RequestNode and true if the cast succeeds, nil and false otherwise.
func AsRequestNode(node AnyNode) (*RequestNode, bool) {
	reqNode, ok := node.(*RequestNode)
	return reqNode, ok
}

func (n *RequestNode) GetData() RequestData {
	return n.Data
}

func (n *RequestNode) GetOutputs() []Output {
	return n.Outputs
}

func (n *RequestNode) GetAssertions() []CompositeAssertion {
	return n.Assertions
}

// InputSchema infers inputs from template variables in URL, Headers, QueryParams, and Body.
func (n *RequestNode) InputSchema() []string {
	si := &SchemaInference{}
	return si.InferRequestNodeInputSchema(n.Data)
}

// OutputSchema infers outputs from the Outputs list.
func (n *RequestNode) OutputSchema() []string {
	si := &SchemaInference{}
	return si.InferRequestNodeOutputSchema(n.GetOutputs())
}

func (n *RequestNode) Execute(ctx spi.ExecutionContext) (spi.AnyResult, error) {
	startTime := time.Now()
	n.dynamic = ctx.DynamicVars

	log.Debug().
		Str("nodeID", n.GetID()).
		Msg("Starting request node execution")

	if err := n.validateInputsPresent(ctx.Inputs); err != nil {
		return n.createErrorResult(ctx.Inputs, err, time.Since(startTime)), err
	}

	exchange, err := n.prepareRequest(ctx.Inputs)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, err, time.Since(startTime)), err
	}

	resp, respBody, err := n.makeRequestAndReadBody(
		ctx.Context(),
		exchange.URL,
		n.Data.Method,
		exchange.Headers,
		exchange.Body,
		n.Data.Timeout,
	)
	if err != nil {
		// The HTTP call limit refused the request, or the response outgrew its limit.
		if limitErr, ok := errors.AsType[*spi.UserError](err); ok {
			return n.createErrorResult(ctx.Inputs, limitErr, time.Since(startTime)), limitErr
		}
		// A transport failure targets a user-configured URL (DNS, connection,
		// TLS, timeout) — the user's endpoint, not a runner fault. Classify it
		// into a clean, user-facing result and let it propagate as a UserError;
		// the engine logs UserErrors at debug rather than tripping error alerts.
		userErr := classifyRequestError(exchange.URL, err)
		return n.createErrorResult(ctx.Inputs, userErr, time.Since(startTime)), userErr
	}
	defer resp.Body.Close()

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("statusCode", resp.StatusCode).
		Int64("bodySize", respBody.size).
		Msg("HTTP response received")

	exchange.Response = resp
	exchange.ResponseBody = respBody.data
	exchange.ResponseSize = respBody.size
	exchange.bodyHold = respBody.hold
	return n.processResponse(ctx.Inputs, exchange, startTime)
}

// processResponse builds the success result for a completed HTTP exchange and
// attaches the ResponseContext its assertions and outputs evaluate against. It no
// longer runs assertions/outputs itself: the engine-level pass drives those
// uniformly via AssertionContextProvider (see engine.applyAssertionsAndOutputs),
// so retry middleware can re-run the node on an assertion failure. Transport
// failures are handled earlier in Execute, before any context exists.
func (n *RequestNode) processResponse(
	inputs map[string]any,
	exchange requestExchange,
	startTime time.Time,
) (spi.AnyResult, error) {
	contentType := exchange.Response.Header.Get("Content-Type")
	var respCtx extractors.ResponseContext
	if exchange.responseSize() > ReportedResponseBodyBytes {
		// The result reports no parsed form of a large body, so only an assertion
		// or output that reads it pays for parsing it.
		body := exchange.ResponseBody
		respCtx = extractors.NewLazyResponseContext(exchange.Response, body, func() any {
			return n.parseResponseBody(contentType, body)
		})
	} else {
		exchange.ParsedBody = n.parseResponseBody(contentType, exchange.ResponseBody)
		respCtx = extractors.NewResponseContext(exchange.Response, exchange.ResponseBody, exchange.ParsedBody)
	}
	result := n.createSuccessResult(inputs, exchange, respCtx, startTime)

	log.Info().
		Str("nodeID", n.GetID()).
		Int("statusCode", exchange.Response.StatusCode).
		Int64("durationMs", result.DurationMs).
		Msg("Request node HTTP exchange completed; deferring assertions/outputs to engine pass")

	return result, nil
}

func (n *RequestNode) createSuccessResult(
	inputs map[string]any,
	exchange requestExchange,
	respCtx extractors.ResponseContext,
	startTime time.Time,
) *RequestExecutionResult {
	body, parsed, truncated := reportedBody(exchange.ResponseBody, exchange.responseSize(), exchange.ParsedBody)
	return &RequestExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    spi.KindRequest,
			Inputs:      inputs,
			Outputs:     map[string]any{},
			ExecutedAt:  time.Now(),
		},
		RequestMethod:         n.Data.Method,
		RequestURL:            exchange.URL,
		RequestHeaders:        exchange.Headers,
		RequestBody:           exchange.Body,
		ResponseStatusCode:    exchange.Response.StatusCode,
		ResponseHeaders:       exchange.Response.Header,
		ResponseBody:          body,
		ResponseBodyParsed:    parsed,
		ResponseBodyTruncated: truncated,
		assertionCtx:          respCtx,
		bodyHold:              exchange.bodyHold,
		DurationMs:            time.Since(startTime).Milliseconds(),
	}
}

// createErrorResult creates a RequestExecutionResult for error cases.
func (n *RequestNode) createErrorResult(
	inputs map[string]any,
	err error,
	duration time.Duration,
) spi.AnyResult {
	errMsg := err.Error()
	errCode := "REQUEST_FAILED"
	// A classified UserError carries a clean, user-facing message and a stable
	// code; surface those instead of the raw Go error string.
	if userErr, ok := spi.AsUserError(err); ok {
		errMsg = userErr.Message
		errCode = userErr.Code
	}

	return &RequestExecutionResult{
		BaseExecutionResult: failedNodeBase(n.BaseNode, inputs, err, failureDetails{
			Kind: spi.KindRequest, Code: errCode, Message: errMsg,
		}),
		DurationMs: duration.Milliseconds(),
	}
}

func (n *RequestNode) createResponseBackedErrorResult(
	inputs map[string]any,
	exchange requestExchange,
	assertionResults []spi.AssertionResult,
	err error,
	duration time.Duration,
) spi.AnyResult {
	result := n.createErrorResult(inputs, err, duration)
	reqResult, ok := result.(*RequestExecutionResult)
	if !ok {
		return result
	}

	reqResult.RequestMethod = n.Data.Method
	reqResult.RequestURL = exchange.URL
	reqResult.RequestHeaders = exchange.Headers
	reqResult.RequestBody = exchange.Body
	if exchange.Response != nil {
		reqResult.ResponseStatusCode = exchange.Response.StatusCode
		reqResult.ResponseHeaders = exchange.Response.Header
	}
	reqResult.ResponseBody, reqResult.ResponseBodyParsed, reqResult.ResponseBodyTruncated = reportedBody(
		exchange.ResponseBody, exchange.responseSize(), exchange.ParsedBody,
	)
	reqResult.AssertionResults = assertionResults

	return reqResult
}

func (n *RequestNode) resolveTemplates(
	value any, inputs map[string]any,
) any {
	resolver := NewTemplateResolverWithDynamics(inputs, n.dynamic)
	resolved, err := resolver.Resolve(value)
	if err != nil {
		return value
	}
	return resolved
}

// resolveTemplatesWithError is like resolveTemplates but returns errors.
func (n *RequestNode) resolveTemplatesWithError(
	value any, inputs map[string]any,
) (string, error) {
	resolver := NewTemplateResolverWithDynamics(inputs, n.dynamic)
	resolved, err := resolver.Resolve(value)
	if err != nil {
		return "", err
	}
	result, ok := resolved.(string)
	if !ok {
		return "", fmt.Errorf("expected string, got %T", resolved)
	}
	return result, nil
}

// makeRequestAndReadBody makes an HTTP request and reads its response body
// within the timeout period. The timeout applies to the entire operation
// (request, waiting for response budget and body read).
func (n *RequestNode) makeRequestAndReadBody(
	parent context.Context, url, method string, headers map[string]string, body any, timeout int,
) (*http.Response, responseBody, error) {
	// An unset/zero timeout means "no explicit timeout"; apply a sane default so the
	// request isn't cancelled with an instant 0ms deadline.
	if timeout <= 0 {
		timeout = defaultRequestTimeoutMs
	}
	if parent == nil {
		parent = context.Background()
	}
	// The per-request timeout layers on top of the caller's context, so flow-level
	// cancellation/deadlines also abort the request.
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, responseBody{}, err
	}
	contentTypeSet := false
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			contentTypeSet = true
			if value == "" {
				continue
			}
		}
		req.Header.Set(key, value)
	}
	if body != nil {
		payload, serializeErr := serializeRequestBody(body)
		if serializeErr != nil {
			return nil, responseBody{}, serializeErr
		}
		if !contentTypeSet && json.Valid(payload) {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Body = io.NopCloser(bytes.NewReader(payload))
		req.ContentLength = int64(len(payload))
	}

	client := recordingClient(n.GetID(), n.Data.URL, false)
	resp, err := client.Do(req)
	if err != nil {
		return nil, responseBody{}, err
	}

	limit := maxResponseBytes(parent)
	// Read the response body while still within the timeout context
	respBody, readErr := readResponseBody(ctx, resp, url, limit, n.keptResponseBytes(limit))
	if readErr != nil {
		_ = resp.Body.Close()
		return nil, responseBody{}, readErr
	}

	return resp, respBody, nil
}

// serializeRequestBody sends a string body as-is: it is already a serialized
// payload, and marshalling it would double-encode it into a quoted string.
func serializeRequestBody(body any) ([]byte, error) {
	if s, ok := body.(string); ok {
		return []byte(s), nil
	}
	return json.Marshal(body)
}
