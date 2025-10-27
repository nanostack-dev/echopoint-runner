package extractors

// BodyExtractor extracts the entire response body.
// It can be used to capture the complete response as a value.
type BodyExtractor struct {
	// No additional fields needed for extracting the full body
}

func (e BodyExtractor) Extract(ctx ResponseContext) (interface{}, error) {
	// Try to get parsed body first (most common case)
	if pbr, ok := ctx.(ParsedBodyReader); ok {
		return pbr.GetParsedBody(), nil
	}

	// If no parsed body, return nil with error
	return nil, ErrNotImplemented
}

func (e BodyExtractor) GetType() ExtractorType {
	return ExtractorTypeBody
}
