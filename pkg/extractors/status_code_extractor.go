package extractors

import "fmt"

// StatusCodeExtractor extracts the HTTP status code from a response
type StatusCodeExtractor struct{}

func (e StatusCodeExtractor) Extract(response interface{}) (interface{}, error) {
	if httpResp, ok := response.(*HTTPResponse); ok {
		return httpResp.StatusCode, nil
	}

	return nil, fmt.Errorf("response is not an HTTP response")
}

func (e StatusCodeExtractor) GetType() ExtractorType {
	return ExtractorTypeStatusCode
}
