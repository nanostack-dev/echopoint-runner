package extractors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderExtractor_GetType(t *testing.T) {
	extractor := HeaderExtractor{HeaderName: "Content-Type"}
	assert.Equal(t, ExtractorTypeHeader, extractor.GetType())
}

func TestHeaderExtractor_Extract_Success(t *testing.T) {
	extractor := HeaderExtractor{HeaderName: "Content-Type"}
	response := &HTTPResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":   "application/json",
			"Content-Length": "1234",
		},
	}

	result, err := extractor.Extract(response)

	require.NoError(t, err)
	assert.Equal(t, "application/json", result)
}

func TestHeaderExtractor_Extract_DifferentHeaders(t *testing.T) {
	testCases := []struct {
		name           string
		headerName     string
		headers        map[string]string
		expectedResult string
	}{
		{
			"Content-Type",
			"Content-Type",
			map[string]string{"Content-Type": "application/json"},
			"application/json",
		},
		{
			"Authorization",
			"Authorization",
			map[string]string{"Authorization": "Bearer token123"},
			"Bearer token123",
		},
		{
			"X-Custom-Header",
			"X-Custom-Header",
			map[string]string{"X-Custom-Header": "custom-value"},
			"custom-value",
		},
		{
			"Cache-Control",
			"Cache-Control",
			map[string]string{"Cache-Control": "no-cache"},
			"no-cache",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			extractor := HeaderExtractor{HeaderName: tc.headerName}
			response := &HTTPResponse{
				StatusCode: 200,
				Headers:    tc.headers,
			}

			result, err := extractor.Extract(response)

			require.NoError(t, err)
			assert.Equal(t, tc.expectedResult, result)
		})
	}
}

func TestHeaderExtractor_Extract_HeaderNotFound(t *testing.T) {
	extractor := HeaderExtractor{HeaderName: "X-Missing-Header"}
	response := &HTTPResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}

	result, err := extractor.Extract(response)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "header X-Missing-Header not found")
}

func TestHeaderExtractor_Extract_EmptyHeaders(t *testing.T) {
	extractor := HeaderExtractor{HeaderName: "Content-Type"}
	response := &HTTPResponse{
		StatusCode: 200,
		Headers:    map[string]string{},
	}

	result, err := extractor.Extract(response)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "header Content-Type not found")
}

func TestHeaderExtractor_Extract_InvalidResponseType(t *testing.T) {
	extractor := HeaderExtractor{HeaderName: "Content-Type"}
	response := "not an HTTP response"

	result, err := extractor.Extract(response)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not an HTTP response")
}

func TestHeaderExtractor_Extract_NilResponse(t *testing.T) {
	extractor := HeaderExtractor{HeaderName: "Content-Type"}

	result, err := extractor.Extract(nil)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestHeaderExtractor_Extract_CaseSensitivity(t *testing.T) {
	extractor := HeaderExtractor{HeaderName: "content-type"}
	response := &HTTPResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json", // Different case
		},
	}

	result, err := extractor.Extract(response)

	// Should fail since header names are case-sensitive in this implementation
	require.Error(t, err)
	assert.Nil(t, result)
}
