package httpextractors_test

import (
	"net/http"
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/internal/logger"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	httpextractors "github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Enable debug logging with human-readable format for tests
	logger.SetDebugLogging()
}

func TestStatusCodeExtractor_GetType(t *testing.T) {
	extractor := httpextractors.StatusCodeExtractor{}
	// StatusCodeExtractor is in http package, needs to return correct type
	assert.NotNil(t, extractor.GetType())
}

func TestStatusCodeExtractor_Extract_Success(t *testing.T) {
	extractor := httpextractors.StatusCodeExtractor{}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"application/json"},
		},
	}

	result, err := extractor.Extract(extractors.NewResponseContext(response, nil, nil))

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
				extractor := httpextractors.StatusCodeExtractor{}
				response := &http.Response{
					StatusCode: tc.statusCode,
				}

				result, err := extractor.Extract(extractors.NewResponseContext(response, nil, nil))

				require.NoError(t, err)
				assert.Equal(t, tc.expectedResult, result)
			},
		)
	}
}

func TestStatusCodeExtractor_Extract_NilContext(t *testing.T) {
	extractor := httpextractors.StatusCodeExtractor{}

	result, err := extractor.Extract(nil)

	require.Error(t, err)
	assert.Nil(t, result)
}
