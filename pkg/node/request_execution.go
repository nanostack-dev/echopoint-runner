package node

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

func (n *RequestNode) validateInputsPresent(inputs map[string]any) error {
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

func (n *RequestNode) prepareRequest(inputs map[string]any) (string, map[string]string, any, error) {
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

func (n *RequestNode) parseResponseBody(contentType string, respBody []byte) any {
	log.Debug().
		Str("nodeID", n.GetID()).
		Str("contentType", contentType).
		Msg("Parsing response body")

	if strings.Contains(contentType, "application/json") {
		var parsedBody any
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
