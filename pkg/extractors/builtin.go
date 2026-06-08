package extractors

import "encoding/json"

// Built-in extractors register themselves here so UnmarshalExtractor is a pure
// registry lookup — adding a new extractor is one RegisterExtractor call (see the
// http package for statusCode/header), no edits to the dispatch core.
//
//nolint:gochecknoinits // register built-in extractors at package load
func init() {
	RegisterExtractor(ExtractorTypeJSONPath, func(data []byte) (AnyExtractor, error) {
		var extractor JSONPathExtractor
		if err := json.Unmarshal(data, &extractor); err != nil {
			return nil, err
		}
		return extractor, nil
	})
	RegisterExtractor(ExtractorTypeXMLPath, func(data []byte) (AnyExtractor, error) {
		var extractor XMLPathExtractor
		if err := json.Unmarshal(data, &extractor); err != nil {
			return nil, err
		}
		return extractor, nil
	})
	RegisterExtractor(ExtractorTypeBody, func(data []byte) (AnyExtractor, error) {
		var extractor BodyExtractor
		if err := json.Unmarshal(data, &extractor); err != nil {
			return nil, err
		}
		return extractor, nil
	})
}
