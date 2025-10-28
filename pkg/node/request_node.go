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

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
)

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

func (n *RequestNode) Execute(ctx ExecutionContext) (map[string]interface{}, error) {
	log.Debug().
		Str("nodeID", n.GetID()).
		Any("inputs", ctx.Inputs).
		Msg("Starting request node execution")

	if err := n.validateInputsPresent(ctx.Inputs); err != nil {
		return nil, err
	}

	url, body, err := n.prepareRequest(ctx.Inputs)
	if err != nil {
		return nil, err
	}

	resp, respBody, err := n.makeRequestAndReadBody(url, n.Data.Method, n.Data.Headers, body, n.Data.Timeout)
	if err != nil {
		log.Error().
			Str("nodeID", n.GetID()).
			Str("method", n.Data.Method).
			Str("url", url).
			Err(err).
			Msg("HTTP request failed")
		return nil, err
	}
	defer resp.Body.Close()

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("statusCode", resp.StatusCode).
		Msg("HTTP response received")

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("bodySize", len(respBody)).
		Msg("Response body read")

	parsedBody := n.parseResponseBody(resp.Header.Get("Content-Type"), respBody)
	respCtx := extractors.NewResponseContext(resp, respBody, parsedBody)

	if assertErr := n.runAssertions(respCtx); assertErr != nil {
		return nil, assertErr
	}

	output, err := n.extractOutputs(respCtx)
	if err != nil {
		return nil, err
	}

	if validateErr := n.validateOutput(output); validateErr != nil {
		return nil, validateErr
	}

	log.Info().
		Str("nodeID", n.GetID()).
		Int("outputCount", len(output)).
		Int("statusCode", resp.StatusCode).
		Msg("Request node executed successfully")

	return output, nil
}

func (n *RequestNode) resolveTemplates(
	value interface{}, inputs map[string]interface{},
) interface{} {
	resolver := NewTemplateResolver(inputs)
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
	resolver := NewTemplateResolver(inputs)
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

func (n *RequestNode) validate(
	_ CompositeAssertion, _ extractors.ResponseContext,
) bool {
	// TODO: Implement validation using extractor and operator factories
	// This requires creating factory functions for extractors and operators
	// For now, return true to allow basic flow execution
	// The context now provides access to status, headers, body, parsed body via interfaces
	return true
}

// makeRequestAndReadBody makes an HTTP request and reads the entire response body
// within the timeout period. The timeout applies to the entire operation (request + body read).
func (n *RequestNode) makeRequestAndReadBody(
	url, method string, headers map[string]string, body interface{}, timeout int,
) (*http.Response, []byte, error) {
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
		jsonBody, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, nil, marshalErr
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
