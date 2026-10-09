package extractors

import (
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/rs/zerolog/log"
)

// XMLPathExtractor extracts values from XML using XPath expressions.
type XMLPathExtractor struct {
	Path string `json:"path"`
}

func (e XMLPathExtractor) Extract(_ ResponseContext) (any, error) {
	log.Debug().
		Str("extractorType", string(spi.ExtractorTypeXMLPath)).
		Str("path", e.Path).
		Msg("Starting XML path extraction")

	// TODO: Implement XPath extraction
	// Use a library like github.com/antchfx/xmlquery or similar
	return nil, ErrNotImplemented
}

func (e XMLPathExtractor) GetType() spi.ExtractorType {
	return spi.ExtractorTypeXMLPath
}
