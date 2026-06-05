package node

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
)

func (n *RequestNode) validateInputsPresent(inputs map[string]interface{}) error {
	for _, dep := range n.InputSchema() {
		if _, exists := inputs[dep]; !exists {
			err := fmt.Errorf("missing required input: %s", dep)
			log.Error().
				Str("nodeID", n.GetID()).
				Str("missingInput", dep).
				Err(err).
				Msg("Input validation failed")
			return err
		}
	}
	return nil
}

func (n *RequestNode) prepareRequest(inputs map[string]interface{}) (string, map[string]string, interface{}, error) {
	log.Debug().
		Str("nodeID", n.GetID()).
		Str("rawURL", n.Data.URL).
		Msg("Resolving URL templates")

	url, err := n.resolveTemplatesWithError(n.Data.URL, inputs)
	if err != nil {
		err = fmt.Errorf("failed to resolve URL templates: %w", err)
		log.Error().
			Str("nodeID", n.GetID()).
			Err(err).
			Msg("URL template resolution failed")
		return "", nil, nil, err
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Str("resolvedURL", url).
		Msg("URL templates resolved successfully")

	// Resolve headers
	headers := make(map[string]string)
	for k, v := range n.Data.Headers {
		resolved := n.resolveTemplates(v, inputs)
		if s, ok := resolved.(string); ok {
			headers[k] = s
		} else {
			headers[k] = fmt.Sprintf("%v", resolved)
		}
	}

	body := n.resolveTemplates(n.Data.Body, inputs)

	// Resolve query parameters and append to URL
	if len(n.Data.QueryParams) > 0 {
		var queryString []string
		for k, v := range n.Data.QueryParams {
			resolvedK := n.resolveTemplates(k, inputs)
			resolvedV := n.resolveTemplates(v, inputs)
			queryString = append(queryString, fmt.Sprintf("%v=%v", resolvedK, resolvedV))
		}
		if strings.Contains(url, "?") {
			url += "&" + strings.Join(queryString, "&")
		} else {
			url += "?" + strings.Join(queryString, "&")
		}
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Str("method", n.Data.Method).
		Str("url", url).
		Int("timeout", n.Data.Timeout).
		Msg("Making HTTP request")

	return url, headers, body, nil
}

func (n *RequestNode) parseResponseBody(contentType string, respBody []byte) interface{} {
	log.Debug().
		Str("nodeID", n.GetID()).
		Str("contentType", contentType).
		Msg("Parsing response body")

	if strings.Contains(contentType, "application/json") {
		var parsedBody interface{}
		if unmarshalErr := json.Unmarshal(respBody, &parsedBody); unmarshalErr != nil {
			log.Warn().
				Str("nodeID", n.GetID()).
				Err(unmarshalErr).
				Msg("JSON parsing failed, treating body as string")
			return string(respBody)
		}
		return parsedBody
	}
	return string(respBody)
}

// runAssertions evaluates every assertion on the node, recording the outcome of
// each (pass or fail) into the returned slice. Evaluation stops at the first
// failing or erroring assertion — that one IS recorded — and the corresponding
// error is returned so the node fails. The slice therefore holds results for all
// assertions up to and including the first failure (or all of them on success).
func (n *RequestNode) runAssertions(
	respCtx extractors.ResponseContext,
) ([]AssertionResult, error) {
	assertions := n.GetAssertions()
	log.Debug().
		Str("nodeID", n.GetID()).
		Int("assertionCount", len(assertions)).
		Msg("Running assertions")

	results := make([]AssertionResult, 0, len(assertions))
	for i := range assertions {
		res := assertions[i].Evaluate(respCtx)
		res.Index = i
		results = append(results, res)

		if res.Error != "" {
			log.Error().
				Str("nodeID", n.GetID()).
				Int("assertionIndex", i).
				Str("extractor", res.Extractor).
				Str("operator", res.Operator).
				Str("error", res.Error).
				Msg("Assertion evaluation errored")
			return results, fmt.Errorf("assertion %d (%s %s) evaluation error: %s",
				i, res.Extractor, res.Operator, res.Error)
		}

		if !res.Passed {
			failedAssertionErr := fmt.Errorf(
				"assertion %d failed: %s %s expected=%v actual=%v",
				i, res.Extractor, res.Operator, res.Expected, res.Actual)
			log.Error().
				Str("nodeID", n.GetID()).
				Int("assertionIndex", i).
				Str("extractor", res.Extractor).
				Str("operator", res.Operator).
				Any("expected", res.Expected).
				Any("actual", res.Actual).
				Err(failedAssertionErr).
				Msg("Assertion failed")
			return results, failedAssertionErr
		}
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Msg("All assertions passed")

	return results, nil
}

func (n *RequestNode) extractOutputs(respCtx extractors.ResponseContext) (map[string]interface{}, error) {
	output := make(map[string]interface{})

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("extractorCount", len(n.GetOutputs())).
		Msg("Extracting outputs")

	for _, outputItem := range n.GetOutputs() {
		log.Debug().
			Str("nodeID", n.GetID()).
			Str("extractorType", string(outputItem.Extractor.GetType())).
			Str("outputName", outputItem.Name).
			Msg("Running extractor")

		value, extractErr := outputItem.Extractor.Extract(respCtx)
		if extractErr != nil {
			log.Error().
				Str("nodeID", n.GetID()).
				Str("outputName", outputItem.Name).
				Str("extractorType", string(outputItem.Extractor.GetType())).
				Err(extractErr).
				Msg("Extraction failed")
			return nil, extractErr
		}
		output[outputItem.Name] = value
		log.Debug().
			Str("nodeID", n.GetID()).
			Str("outputName", outputItem.Name).
			Any("value", value).
			Msg("Output extracted successfully")
	}

	return output, nil
}

func (n *RequestNode) validateOutput(output map[string]interface{}) error {
	for _, expectedKey := range n.OutputSchema() {
		if _, exists := output[expectedKey]; !exists {
			errOutput := fmt.Errorf("failed to extract expected output: %s", expectedKey)
			log.Error().
				Str("nodeID", n.GetID()).
				Str("expectedOutput", expectedKey).
				Err(errOutput).
				Msg("Output validation failed")
			return errOutput
		}
	}
	return nil
}
