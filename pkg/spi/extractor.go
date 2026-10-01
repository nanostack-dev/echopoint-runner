package spi

// ExtractorType identifies an output/assertion extractor on the wire. The
// extractor implementations live in pkg/extractors and self-register; this is
// just the wire identifier the contract binds to.
type ExtractorType string

// Built-in extractor types.
const (
	ExtractorTypeJSONPath   ExtractorType = "json_path"
	ExtractorTypeXMLPath    ExtractorType = "xml_path"
	ExtractorTypeStatusCode ExtractorType = "status_code"
	ExtractorTypeHeader     ExtractorType = "header"
	ExtractorTypeBody       ExtractorType = "body"
	ExtractorTypeQueryParam ExtractorType = "query_param"
)
