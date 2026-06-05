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
)

// defaultRequestTimeoutMs is applied when a request node has no explicit timeout
// (Timeout <= 0), avoiding an instant 0ms context deadline.
const defaultRequestTimeoutMs = 30000

type RequestData struct {
	Method      string                 `json:"method"`
	URL         string                 `json:"url"`
	Headers     map[string]string      `json:"headers"`
	QueryParams map[string]interface{} `json:"queryParams"`
	Body        interface{}            `json:"body"`
	Timeout     int                    `json:"timeout"`
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

	resp, respBody, err := n.makeRequestAndReadBody(url, n.Data.Method, headers, body, n.Data.Timeout)
	if err != nil {
		log.Error().
			Str("nodeID", n.GetID()).
			Str("method", n.Data.Method).
			Str("url", url).
			Err(err).
			Msg("HTTP request failed")
		return n.createErrorResult(ctx.Inputs, err, time.Since(startTime)), err
	}
	defer resp.Body.Close()

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("statusCode", resp.StatusCode).
		Int("bodySize", len(respBody)).
		Msg("HTTP response received")

	return n.processResponse(ctx.Inputs, url, headers, body, resp, respBody, startTime)
}

// processResponse runs assertions, extracts and validates outputs, and builds the
// final result for a completed HTTP exchange. Any of the three failure points
// produces a response-backed error result carrying the assertions evaluated so far.
func (n *RequestNode) processResponse(
	inputs map[string]interface{},
	url string,
	headers map[string]string,
	body interface{},
	resp *http.Response,
	respBody []byte,
	startTime time.Time,
) (AnyExecutionResult, error) {
	parsedBody := n.parseResponseBody(resp.Header.Get("Content-Type"), respBody)
	respCtx := extractors.NewResponseContext(resp, respBody, parsedBody)

	assertionResults, assertErr := n.runAssertions(respCtx)
	errResult := func(err error) (AnyExecutionResult, error) {
		return n.createResponseBackedErrorResult(
			inputs, url, headers, body, resp, respBody, parsedBody, assertionResults, err, time.Since(startTime),
		), err
	}
	if assertErr != nil {
		return errResult(assertErr)
	}

	outputs, err := n.extractOutputs(respCtx)
	if err != nil {
		return errResult(err)
	}

	if validateErr := n.validateOutput(outputs); validateErr != nil {
		return errResult(validateErr)
	}

	result := n.createSuccessResult(
		inputs, outputs, url, headers, body, resp, respBody, parsedBody, assertionResults, startTime,
	)

	log.Info().
		Str("nodeID", n.GetID()).
		Int("outputCount", len(outputs)).
		Int("statusCode", resp.StatusCode).
		Int64("durationMs", result.DurationMs).
		Msg("Request node executed successfully")

	return result, nil
}

func (n *RequestNode) createSuccessResult(
	inputs map[string]interface{},
	outputs map[string]interface{},
	url string,
	headers map[string]string,
	body interface{},
	resp *http.Response,
	respBody []byte,
	parsedBody interface{},
	assertionResults []AssertionResult,
	startTime time.Time,
) *RequestExecutionResult {
	return &RequestExecutionResult{
		BaseExecutionResult: BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    TypeRequest,
			Inputs:      inputs,
			Outputs:     outputs,
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
		AssertionResults:   assertionResults,
		DurationMs:         time.Since(startTime).Milliseconds(),
	}
}

// createErrorResult creates a RequestExecutionResult for error cases.
func (n *RequestNode) createErrorResult(
	inputs map[string]interface{},
	err error,
	duration time.Duration,
) AnyExecutionResult {
	errMsg := err.Error()
	errCode := "REQUEST_FAILED"

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
	inputs map[string]interface{},
	url string,
	headers map[string]string,
	body interface{},
	resp *http.Response,
	respBody []byte,
	parsedBody interface{},
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
	value interface{}, inputs map[string]interface{},
) interface{} {
	resolver := NewTemplateResolverWithDynamics(inputs, n.dynamic)
	resolved, err := resolver.Resolve(value)
	if err != nil {
		return value
	}
	return resolved
}

// resolveTemplatesWithError is like resolveTemplates but returns errors.
func (n *RequestNode) resolveTemplatesWithError(
	value interface{}, inputs map[string]interface{},
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
	url, method string, headers map[string]string, body interface{}, timeout int,
) (*http.Response, []byte, error) {
	// An unset/zero timeout means "no explicit timeout"; apply a sane default so the
	// request isn't cancelled with an instant 0ms deadline.
	if timeout <= 0 {
		timeout = defaultRequestTimeoutMs
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
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
