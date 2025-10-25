package extractors

import "errors"

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

var ErrNotImplemented = errors.New("extractor not implemented")
