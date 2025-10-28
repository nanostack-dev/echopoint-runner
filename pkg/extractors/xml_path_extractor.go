package extractors

import (
	"github.com/rs/zerolog/log"
)

// XMLPathExtractor extracts values from XML using XPath expressions.
type XMLPathExtractor struct {
	Path string `json:"path"`
}

func (e XMLPathExtractor) Extract(_ ResponseContext) (interface{}, error) {
	log.Debug().
		Str("extractorType", string(ExtractorTypeXMLPath)).
		Str("path", e.Path).
		Msg("Starting XML path extraction")

	// TODO: Implement XPath extraction
	// Use a library like github.com/antchfx/xmlquery or similar
	log.Error().
		Str("extractorType", string(ExtractorTypeXMLPath)).
		Str("path", e.Path).
		Err(ErrNotImplemented).
		Msg("XML path extraction not implemented")
	return nil, ErrNotImplemented
}

func (e XMLPathExtractor) GetType() ExtractorType {
	return ExtractorTypeXMLPath
}
