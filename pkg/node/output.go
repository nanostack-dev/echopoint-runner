package node

import (
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	_ "github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors/http"
)

// UnmarshalJSON implements custom unmarshaling for Output
// This allows us to properly unmarshal the Extractor field from JSON
func (o *Output) UnmarshalJSON(data []byte) error {
	type Alias Output
	aux := &struct {
		Extractor json.RawMessage `json:"extractor"`
		*Alias
	}{
		Alias: (*Alias)(o),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if len(aux.Extractor) > 0 {
		extractor, err := extractors.UnmarshalExtractor(aux.Extractor)
		if err != nil {
			return fmt.Errorf("failed to unmarshal extractor: %w", err)
		}
		o.Extractor = extractor
	}

	return nil
}
