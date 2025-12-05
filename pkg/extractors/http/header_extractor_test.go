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

// Helper to create a ResponseContext from an http.Response.
func createTestContext(resp *http.Response) extractors.ResponseContext {
	return extractors.NewResponseContext(resp, nil, nil)
}

func TestHeaderExtractor_GetType(t *testing.T) {
	extractor := httpextractors.HeaderExtractor{HeaderName: "Content-Type"}
	assert.NotNil(t, extractor.GetType())
}

func TestHeaderExtractor_Extract_Success(t *testing.T) {
	extractor := httpextractors.HeaderExtractor{HeaderName: "Content-Type"}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":   {"application/json"},
			"Content-Length": {"1234"},
		},
	}

	result, err := extractor.Extract(createTestContext(response))

	require.NoError(t, err)
	assert.Equal(t, "application/json", result)
}

func TestHeaderExtractor_Extract_DifferentHeaders(t *testing.T) {
	testCases := []struct {
		name           string
		headerName     string
		headerValue    string
		expectedResult string
	}{
		{
			"Content-Type",
			"Content-Type",
			"application/json",
			"application/json",
		},
		{
			"Authorization",
			"Authorization",
			"Bearer token123",
			"Bearer token123",
		},
		{
			"X-Custom-Header",
			"X-Custom-Header",
			"custom-value",
			"custom-value",
		},
		{
			"Cache-Control",
			"Cache-Control",
			"no-cache",
			"no-cache",
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				extractor := httpextractors.HeaderExtractor{HeaderName: tc.headerName}
				response := &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						tc.headerName: {tc.headerValue},
					},
				}

				result, err := extractor.Extract(createTestContext(response))

				require.NoError(t, err)
				assert.Equal(t, tc.expectedResult, result)
			},
		)
	}
}

func TestHeaderExtractor_Extract_HeaderNotFound(t *testing.T) {
	extractor := httpextractors.HeaderExtractor{HeaderName: "X-Missing-Header"}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"application/json"},
		},
	}

	result, err := extractor.Extract(createTestContext(response))

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "header X-Missing-Header not found")
}

func TestHeaderExtractor_Extract_EmptyHeaders(t *testing.T) {
	extractor := httpextractors.HeaderExtractor{HeaderName: "Content-Type"}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
	}

	result, err := extractor.Extract(createTestContext(response))

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "header Content-Type not found")
}

func TestHeaderExtractor_Extract_NilContext(t *testing.T) {
	extractor := httpextractors.HeaderExtractor{HeaderName: "Content-Type"}

	result, err := extractor.Extract(nil)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestHeaderExtractor_Extract_CaseInsensitivity(t *testing.T) {
	extractor := httpextractors.HeaderExtractor{HeaderName: "content-type"}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"application/json"}, // Different case
		},
	}

	result, err := extractor.Extract(createTestContext(response))

	// http.Header.Get() is case-insensitive, so this should succeed
	require.NoError(t, err)
	assert.Equal(t, "application/json", result)
}
