package extractors

// XMLPathExtractor extracts values from XML using XPath expressions.
type XMLPathExtractor struct {
	Path string `json:"path"`
}

func (e XMLPathExtractor) Extract(_ interface{}) (interface{}, error) {
	// TODO: Implement XPath extraction
	// Use a library like github.com/antchfx/xmlquery or similar
	return nil, ErrNotImplemented
}

func (e XMLPathExtractor) GetType() ExtractorType {
	return ExtractorTypeXMLPath
}
