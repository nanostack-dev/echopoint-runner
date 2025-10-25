package extractors_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusCodeExtractor_GetType(t *testing.T) {
	extractor := extractors.StatusCodeExtractor{}
	assert.Equal(t, extractors.ExtractorTypeStatusCode, extractor.GetType())
}

func TestStatusCodeExtractor_Extract_Success(t *testing.T) {
	extractor := extractors.StatusCodeExtractor{}
	response := &extractors.HTTPResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}

	result, err := extractor.Extract(response)

	require.NoError(t, err)
	assert.Equal(t, 200, result)
}

func TestStatusCodeExtractor_Extract_DifferentStatusCodes(t *testing.T) {
	testCases := []struct {
		name           string
		statusCode     int
		expectedResult int
	}{
		{"OK", 200, 200},
		{"Created", 201, 201},
		{"No Content", 204, 204},
		{"Bad Request", 400, 400},
		{"Unauthorized", 401, 401},
		{"Forbidden", 403, 403},
		{"Not Found", 404, 404},
		{"Internal Server Error", 500, 500},
		{"Bad Gateway", 502, 502},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				extractor := extractors.StatusCodeExtractor{}
				response := &extractors.HTTPResponse{
					StatusCode: tc.statusCode,
				}

				result, err := extractor.Extract(response)

				require.NoError(t, err)
				assert.Equal(t, tc.expectedResult, result)
			},
		)
	}
}

func TestStatusCodeExtractor_Extract_InvalidResponseType(t *testing.T) {
	extractor := extractors.StatusCodeExtractor{}
	response := "not an HTTP response"

	result, err := extractor.Extract(response)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not an HTTP response")
}

func TestStatusCodeExtractor_Extract_NilResponse(t *testing.T) {
	extractor := extractors.StatusCodeExtractor{}

	result, err := extractor.Extract(nil)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestStatusCodeExtractor_Extract_MapResponse(t *testing.T) {
	extractor := extractors.StatusCodeExtractor{}
	response := map[string]interface{}{
		"statusCode": 200,
	}

	result, err := extractor.Extract(response)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not an HTTP response")
}
