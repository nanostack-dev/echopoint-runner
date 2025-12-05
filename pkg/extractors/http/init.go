package httpextractors

import (
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
)

//nolint:gochecknoinits
func init() {
	// Register StatusCodeExtractor
	extractors.RegisterExtractor(
		extractors.ExtractorTypeStatusCode,
		func(data []byte) (extractors.AnyExtractor, error) {
			var extractor StatusCodeExtractor
			if err := json.Unmarshal(data, &extractor); err != nil {
				return nil, fmt.Errorf("failed to unmarshal StatusCode extractor: %w", err)
			}
			return extractor, nil
		},
	)

	// Register HeaderExtractor
	extractors.RegisterExtractor(extractors.ExtractorTypeHeader, func(data []byte) (extractors.AnyExtractor, error) {
		var extractor HeaderExtractor
		if err := json.Unmarshal(data, &extractor); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Header extractor: %w", err)
		}
		return extractor, nil
	})
}
