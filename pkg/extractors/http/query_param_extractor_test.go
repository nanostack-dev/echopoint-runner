package httpextractors_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	httpextractors "github.com/nanostack-dev/echopoint-runner/pkg/extractors/http"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type queryCtx map[string]string

func (q queryCtx) HasCapability(capability string) bool { return capability == "query_params" }

func (q queryCtx) GetQueryParam(name string) (string, bool) {
	value, found := q[name]
	return value, found
}

func TestQueryParamExtractor_ReadsTheParam(t *testing.T) {
	got, err := httpextractors.QueryParamExtractor{ParamName: "q"}.Extract(queryCtx{"q": "x"})
	require.NoError(t, err)
	assert.Equal(t, "x", got)
}

func TestQueryParamExtractor_PresentButEmptyIsFound(t *testing.T) {
	got, err := httpextractors.QueryParamExtractor{ParamName: "q"}.Extract(queryCtx{"q": ""})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestQueryParamExtractor_MissingParamErrors(t *testing.T) {
	_, err := httpextractors.QueryParamExtractor{ParamName: "q"}.Extract(queryCtx{})
	require.ErrorContains(t, err, "query param q not found")
}

func TestQueryParamExtractor_ResponseHasNoQueryParams(t *testing.T) {
	ctx := extractors.NewResponseContext(&http.Response{Header: http.Header{}}, nil, nil)
	_, err := httpextractors.QueryParamExtractor{ParamName: "q"}.Extract(ctx)
	require.Error(t, err)
}

func TestQueryParamExtractor_DecodesFromTheWire(t *testing.T) {
	extractor, err := extractors.UnmarshalExtractor([]byte(`{"type":"query_param","param_name":"q"}`))
	require.NoError(t, err)
	assert.Equal(t, httpextractors.QueryParamExtractor{ParamName: "q"}, extractor)
	assert.Equal(t, spi.ExtractorTypeQueryParam, extractor.GetType())
}
