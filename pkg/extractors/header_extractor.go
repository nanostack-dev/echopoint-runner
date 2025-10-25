package extractors

import "fmt"

// HeaderExtractor extracts HTTP header values from a response
type HeaderExtractor struct {
	HeaderName string `json:"headerName"`
}

func (e HeaderExtractor) Extract(response interface{}) (interface{}, error) {
	if httpResp, ok := response.(*HTTPResponse); ok {
		if value, exists := httpResp.Headers[e.HeaderName]; exists {
			return value, nil
		}
		return nil, fmt.Errorf("header %s not found", e.HeaderName)
	}

	return nil, fmt.Errorf("response is not an HTTP response")
}

func (e HeaderExtractor) GetType() ExtractorType {
	return ExtractorTypeHeader
}
