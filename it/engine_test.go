package it_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/it/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWireMockStub_GetUsers200(t *testing.T) {
	ctx := shared.GetFlowEngineContext()
	require.NotNil(t, ctx, "test context should be initialized")

	// Make request to WireMock stub
	resp, err := http.Get(ctx.WireMockURL + "/users")
	require.NoError(t, err, "request should not error")
	defer resp.Body.Close()

	// Assert status code
	assert.Equal(t, http.StatusOK, resp.StatusCode, "should return 200")

	// Assert content type
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Read and verify response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "should read response body")

	// Verify response contains expected data
	assert.Contains(t, string(body), "John Doe")
	assert.Contains(t, string(body), "Jane Smith")
}
