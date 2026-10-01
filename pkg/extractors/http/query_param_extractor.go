package httpextractors

import (
	"errors"
	"fmt"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// QueryParamExtractor extracts a query parameter of a received request, such as
// the request a webhook wait matched. An HTTP response has no query parameters.
type QueryParamExtractor struct {
	ParamName string `json:"param_name"`
}

func (e QueryParamExtractor) Extract(ctx extractors.ResponseContext) (any, error) {
	accessor, ok := ctx.(extractors.QueryParamAccessor)
	if !ok {
		return nil, errors.New("context does not implement QueryParamAccessor interface")
	}
	value, found := accessor.GetQueryParam(e.ParamName)
	if !found {
		return nil, fmt.Errorf("query param %s not found", e.ParamName)
	}
	return value, nil
}

func (e QueryParamExtractor) GetType() spi.ExtractorType {
	return spi.ExtractorTypeQueryParam
}
