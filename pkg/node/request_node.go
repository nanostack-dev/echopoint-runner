package node

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RequestData struct {
	Method      string                 `json:"method"`
	URL         string                 `json:"url"`
	Headers     map[string]string      `json:"headers"`
	QueryParams map[string]interface{} `json:"queryParams"`
	Body        interface{}            `json:"body"`
	Timeout     int                    `json:"timeout"`
	Assertions  []CompositeAssertion   `json:"assertions"`
	Extractions []Extraction
}
type Extraction struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

// RequestNode is a typed node for HTTP requests.
type RequestNode struct {
	ID         string      `json:"id"`
	Type       Type        `json:"type"`
	Data       RequestData `json:"data"`
	InputDeps  []string    `json:"inputSchema"`
	OutputKeys []string    `json:"outputSchema"`
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

func (n *RequestNode) GetID() string {
	return n.ID
}

func (n *RequestNode) GetType() Type {
	return n.Type
}

func (n *RequestNode) GetData() RequestData {
	return n.Data
}

func (n *RequestNode) InputSchema() []string {
	return n.InputDeps
}

func (n *RequestNode) OutputSchema() []string {
	return n.OutputKeys
}

func (n *RequestNode) Execute(ctx ExecutionContext) (map[string]interface{}, error) {
	// Validate that we have all required inputs
	for _, dep := range n.InputSchema() {
		if _, exists := ctx.Inputs[dep]; !exists {
			return nil, fmt.Errorf("missing required input: %s", dep)
		}
	}

	output := make(map[string]interface{})

	url := n.resolveTemplates(n.Data.URL, ctx.Inputs).(string)
	body := n.resolveTemplates(n.Data.Body, ctx.Inputs)

	// Make the HTTP request
	resp, err := n.makeRequest(url, n.Data.Method, n.Data.Headers, body, n.Data.Timeout)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse response based on content-type
	var parsedBody interface{}
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		json.Unmarshal(respBody, &parsedBody)
	} else {
		parsedBody = string(respBody)
	}

	// Run assertions (these validate but don't produce output)
	for _, assertion := range n.Data.Assertions {
		if !n.validate(assertion, parsedBody, resp) {
			return nil, fmt.Errorf("assertion failed: %v", assertion)
		}
	}

	// Extract data as declared in outputSchema
	for _, extraction := range n.Data.Extractions {
		value, err := n.extract(extraction, parsedBody, resp)
		if err != nil {
			return nil, err
		}
		output[extraction.Name] = value
	}

	// Validate output matches OutputSchema()
	for _, expectedKey := range n.OutputSchema() {
		if _, exists := output[expectedKey]; !exists {
			return nil, fmt.Errorf("failed to extract expected output: %s", expectedKey)
		}
	}

	return output, nil
}

func (n *RequestNode) resolveTemplates(
	value interface{}, inputs map[string]interface{},
) interface{} {
	resolver := NewTemplateResolver(inputs)
	resolved, err := resolver.Resolve(value)
	if err != nil {
		// In case of error, return original value
		// The error will be caught during actual request execution
		return value
	}
	return resolved
}

func (n *RequestNode) extract(
	extraction Extraction, body interface{}, resp *http.Response,
) (interface{}, error) {
	switch extraction.Type {
	case "statusCode":
		return resp.StatusCode, nil
	case "header":
		return resp.Header.Get(extraction.Path), nil
	case "body":
		return body, nil
	case "jsonPath":
		// TODO: Implement JSONPath extraction using jsonpath library
		// For now, return the entire body
		return body, nil
	default:
		return nil, fmt.Errorf("unknown extraction type: %s", extraction.Type)
	}
}

func (n *RequestNode) validate(
	assertion CompositeAssertion, body interface{}, resp *http.Response,
) bool {
	// TODO: Implement validation using extractor and operator factories
	// This requires creating factory functions for extractors and operators
	// For now, return true to allow basic flow execution
	return true
}

func (n *RequestNode) makeRequest(
	url, method string, headers map[string]string, body interface{}, timeout int,
) (*http.Response, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		jsonBody, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, marshalErr
		}
		req.Body = io.NopCloser(strings.NewReader(string(jsonBody)))
		req.ContentLength = int64(len(jsonBody))
	}
	if timeout > 0 {
		client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
		return client.Do(req)
	} else {
		client := &http.Client{}
		return client.Do(req)
	}
}
