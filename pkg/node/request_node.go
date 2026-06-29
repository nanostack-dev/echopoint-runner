package node

import (
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

// defaultRequestTimeoutMs is applied when a request node has no explicit timeout
// (Timeout <= 0), avoiding an instant 0ms context deadline.
const defaultRequestTimeoutMs = 30000

type RequestData struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	QueryParams map[string]any    `json:"queryParams"`
	Body        any               `json:"body"`
	Timeout     int               `json:"timeout"`
}

// RequestNode is a typed node for HTTP requests.
type RequestNode struct {
	BaseNode

	Data RequestData `json:"data"`

	// dynamic resolves {{$name}} variables; set per execution, not serialized.
	dynamic DynamicResolver
}

// AsRequestNode safely casts an AnyNode to a RequestNode
// Returns the RequestNode and true if the cast succeeds, nil and false otherwise.
func AsRequestNode(node AnyNode) (*RequestNode, bool) {
	reqNode, ok := node.(*RequestNode)
	return reqNode, ok
}

// MustAsRequestNode casts an AnyNode to a RequestNode, panicking if it fails
// Use this when you're certain the node is a RequestNode.
func MustAsRequestNode(node AnyNode) *RequestNode {
	reqNode, ok := AsRequestNode(node)
	if !ok {
		panic("expected RequestNode but got different type")
	}
	return reqNode
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

func (n *RequestNode) Execute(ctx ExecutionContext) (AnyExecutionResult, error) {
	startTime := time.Now()
	n.dynamic = ctx.DynamicVars

	log.Debug().
		Str("nodeID", n.GetID()).
		Any("inputs", ctx.Inputs).
		Msg("Starting request node execution")

	if err := n.validateInputsPresent(ctx.Inputs); err != nil {
		return n.createErrorResult(ctx.Inputs, err, time.Since(startTime)), err
	}

	url, headers, body, err := n.prepareRequest(ctx.Inputs)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, err, time.Since(startTime)), err
	}

	resp, respBody, err := n.makeRequestAndReadBody(ctx.Context(), url, n.Data.Method, headers, body, n.Data.Timeout)
	if err != nil {
		// A transport failure targets a user-configured URL (DNS, connection,
		// TLS, timeout) — the user's endpoint, not a runner fault. Classify it
		// into a clean, user-facing result and let it propagate as a UserError;
		// the engine logs UserErrors at debug rather than tripping error alerts.
		userErr := classifyRequestError(url, err)
		return n.createErrorResult(ctx.Inputs, userErr, time.Since(startTime)), userErr
	}
	defer resp.Body.Close()

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("statusCode", resp.StatusCode).
		Int("bodySize", len(respBody)).
		Msg("HTTP response received")

	return n.processResponse(ctx.Inputs, url, headers, body, resp, respBody, startTime)
}

// processResponse builds the success result for a completed HTTP exchange and
// attaches the ResponseContext its assertions and outputs evaluate against. It no
// longer runs assertions/outputs itself: the engine-level pass drives those
// uniformly via AssertionContextProvider (see engine.applyAssertionsAndOutputs),
// so retry middleware can re-run the node on an assertion failure. Transport
// failures are handled earlier in Execute, before any context exists.
func (n *RequestNode) processResponse(
	inputs map[string]any,
	url string,
	headers map[string]string,
	body any,
	resp *http.Response,
	respBody []byte,
	startTime time.Time,
) (AnyExecutionResult, error) {
	parsedBody := n.parseResponseBody(resp.Header.Get("Content-Type"), respBody)
	respCtx := extractors.NewResponseContext(resp, respBody, parsedBody)

	result := n.createSuccessResult(
		inputs, url, headers, body, resp, respBody, parsedBody, respCtx, startTime,
	)

	log.Info().
		Str("nodeID", n.GetID()).
		Int("statusCode", resp.StatusCode).
		Int64("durationMs", result.DurationMs).
		Msg("Request node HTTP exchange completed; deferring assertions/outputs to engine pass")

	return result, nil
}

func (n *RequestNode) createSuccessResult(
	inputs map[string]any,
	url string,
	headers map[string]string,
	body any,
	resp *http.Response,
	respBody []byte,
	parsedBody any,
	respCtx extractors.ResponseContext,
	startTime time.Time,
) *RequestExecutionResult {
	return &RequestExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeRequest,
			Inputs:      inputs,
			Outputs:     map[string]any{},
			ExecutedAt:  time.Now(),
		},
		RequestMethod:      n.Data.Method,
		RequestURL:         url,
		RequestHeaders:     headers,
		RequestBody:        body,
		ResponseStatusCode: resp.StatusCode,
		ResponseHeaders:    resp.Header,
		ResponseBody:       respBody,
		ResponseBodyParsed: parsedBody,
		assertionCtx:       respCtx,
		DurationMs:         time.Since(startTime).Milliseconds(),
	}
}

// createErrorResult creates a RequestExecutionResult for error cases.
func (n *RequestNode) createErrorResult(
	inputs map[string]any,
	err error,
	duration time.Duration,
) AnyExecutionResult {
	errMsg := err.Error()
	errCode := "REQUEST_FAILED"
	// A classified UserError carries a clean, user-facing message and a stable
	// code; surface those instead of the raw Go error string.
	if userErr, ok := spi.AsUserError(err); ok {
		errMsg = userErr.Message
		errCode = userErr.Code
	}

	return &RequestExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeRequest,
			Inputs:      inputs,
			Outputs:     nil,
			Error:       err,
			ErrorMsg:    &errMsg,
			ErrorCode:   &errCode,
			ExecutedAt:  time.Now(),
		},
		DurationMs: duration.Milliseconds(),
	}
}

func (n *RequestNode) createResponseBackedErrorResult(
	inputs map[string]any,
	url string,
	headers map[string]string,
	body any,
	resp *http.Response,
	respBody []byte,
	parsedBody any,
	assertionResults []AssertionResult,
	err error,
	duration time.Duration,
) AnyExecutionResult {
	result := n.createErrorResult(inputs, err, duration)
	reqResult, ok := result.(*RequestExecutionResult)
	if !ok {
		return result
	}

	reqResult.RequestMethod = n.Data.Method
	reqResult.RequestURL = url
	reqResult.RequestHeaders = headers
	reqResult.RequestBody = body
	if resp != nil {
		reqResult.ResponseStatusCode = resp.StatusCode
		reqResult.ResponseHeaders = resp.Header
	}
	reqResult.ResponseBody = respBody
	reqResult.ResponseBodyParsed = parsedBody
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

// makeRequestAndReadBody makes an HTTP request and reads the entire response body
// within the timeout period. The timeout applies to the entire operation (request + body read).
func (n *RequestNode) makeRequestAndReadBody(
	parent context.Context, url, method string, headers map[string]string, body any, timeout int,
) (*http.Response, []byte, error) {
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
		return nil, nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		// A string body is already a serialized payload (e.g. a JSON object literal);
		// send it as-is. Marshalling it would double-encode it into a quoted string.
		var jsonBody []byte
		if s, ok := body.(string); ok {
			jsonBody = []byte(s)
		} else {
			marshalled, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				return nil, nil, marshalErr
			}
			jsonBody = marshalled
		}
		req.Body = io.NopCloser(strings.NewReader(string(jsonBody)))
		req.ContentLength = int64(len(jsonBody))
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	// Read the response body while still within the timeout context
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		_ = resp.Body.Close()
		return nil, nil, readErr
	}

	return resp, respBody, nil
}
