package extractors

// HTTPResponse represents an HTTP response for extraction.
type HTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       interface{}
}
