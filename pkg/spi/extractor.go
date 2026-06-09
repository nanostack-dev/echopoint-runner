package spi

// ExtractorType identifies an output/assertion extractor on the wire. The
// extractor implementations live in pkg/extractors and self-register; this is
// just the wire identifier the contract binds to.
type ExtractorType string

// Built-in extractor types.
const (
	ExtractorTypeJSONPath   ExtractorType = "jsonPath"
	ExtractorTypeXMLPath    ExtractorType = "xmlPath"
	ExtractorTypeStatusCode ExtractorType = "statusCode"
	ExtractorTypeHeader     ExtractorType = "header"
	ExtractorTypeBody       ExtractorType = "body"
)
