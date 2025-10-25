package extractors

type Extractor interface {
	Extract(response interface{}) (interface{}, error)
	GetType() ExtractorType
}

type ExtractorType string

const (
	ExtractorTypeJSONPath   ExtractorType = "jsonPath"
	ExtractorTypeXMLPath    ExtractorType = "xmlPath"
	ExtractorTypeStatusCode ExtractorType = "statusCode"
	ExtractorTypeHeader     ExtractorType = "header"
)
