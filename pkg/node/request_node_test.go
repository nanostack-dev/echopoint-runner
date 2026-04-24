package node_test

import (
	"net/http"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareRequest_PreservesRawJSONTemplateBodyValue(t *testing.T) {
	reqNode := &node.RequestNode{
		BaseNode: node.BaseNode{ID: "step1", DisplayName: "Search Permissions", NodeType: node.TypeRequest},
		Data: node.RequestData{
			Method: http.MethodPost,
			URL:    "https://example.com/permissions",
			Body: map[string]any{
				"permissions": "{{{search_product_permissions.allPermissionNames}}}",
			},
			Timeout: 1000,
		},
	}

	url, headers, body, err := node.PrepareRequestForTest(reqNode, map[string]any{
		"search_product_permissions.allPermissionNames": []any{"organization:read", "organization:create"},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/permissions", url)
	assert.Empty(t, headers)
	assert.Equal(t, map[string]any{
		"permissions": []any{"organization:read", "organization:create"},
	}, body)
}

func TestPrepareRequest_PreservesRawJSONObjectAndScalarTemplateBodyValues(t *testing.T) {
	reqNode := &node.RequestNode{
		BaseNode: node.BaseNode{ID: "step1", DisplayName: "Create Product", NodeType: node.TypeRequest},
		Data: node.RequestData{
			Method: http.MethodPost,
			URL:    "https://example.com/products",
			Body: map[string]any{
				"filters": "{{{search.filters}}}",
				"enabled": "{{{flags.enabled}}}",
				"count":   "{{{flags.count}}}",
				"name":    "{{{product.name}}}",
			},
			Timeout: 1000,
		},
	}

	_, _, body, err := node.PrepareRequestForTest(reqNode, map[string]any{
		"search.filters": map[string]any{"active": true, "limit": float64(10)},
		"flags.enabled":  true,
		"flags.count":    float64(42),
		"product.name":   "alexis",
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"filters": map[string]any{"active": true, "limit": float64(10)},
		"enabled": true,
		"count":   float64(42),
		"name":    "alexis",
	}, body)
}

func TestPrepareRequest_LeavesDoubleBraceTemplatesStringBased(t *testing.T) {
	reqNode := &node.RequestNode{
		BaseNode: node.BaseNode{ID: "step1", DisplayName: "Create Product", NodeType: node.TypeRequest},
		Data: node.RequestData{
			Method: http.MethodPost,
			URL:    "{{baseUrl}}/products",
			Headers: map[string]string{
				"Authorization": "Bearer {{token}}",
			},
			Body: map[string]any{
				"message": "Hello {{userName}}",
			},
			Timeout: 1000,
		},
	}

	url, headers, body, err := node.PrepareRequestForTest(reqNode, map[string]any{
		"baseUrl":  "https://example.com",
		"token":    "abc123",
		"userName": "alexis",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/products", url)
	assert.Equal(t, map[string]string{"Authorization": "Bearer abc123"}, headers)
	assert.Equal(t, map[string]any{"message": "Hello alexis"}, body)
}

func TestPrepareRequest_LeavesUnknownRawTemplateLiteral(t *testing.T) {
	reqNode := &node.RequestNode{
		BaseNode: node.BaseNode{ID: "step1", DisplayName: "Create Product", NodeType: node.TypeRequest},
		Data: node.RequestData{
			Method: http.MethodPost,
			URL:    "https://example.com/products",
			Body: map[string]any{
				"permissions": "{{{missing.permissions}}}",
			},
			Timeout: 1000,
		},
	}

	_, _, body, err := node.PrepareRequestForTest(reqNode, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"permissions": "{{{missing.permissions}}}"}, body)
}

func TestCreateResponseBackedErrorResultPreservesHTTPContext(t *testing.T) {
	reqNode := &node.RequestNode{
		BaseNode: node.BaseNode{
			ID:          "step1",
			DisplayName: "Step 1",
			NodeType:    node.TypeRequest,
		},
		Data: node.RequestData{Method: http.MethodPost},
	}

	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}
	respBody := []byte(`{"error":"invalid credentials"}`)
	parsedBody := map[string]any{"error": "invalid credentials"}
	_, err := extractors.JSONPathExtractor{Path: "$.id"}.Extract(
		extractors.NewResponseContext(resp, respBody, parsedBody),
	)
	require.Error(t, err)

	result, ok := node.CreateResponseBackedErrorResultForTest(
		reqNode,
		map[string]any{"email": "alice@example.com"},
		"https://example.com/login",
		map[string]string{"Authorization": "Bearer token"},
		map[string]any{"email": "alice@example.com"},
		resp,
		respBody,
		parsedBody,
		err,
		0,
	).(*node.RequestExecutionResult)
	require.True(t, ok)

	assert.Equal(t, "REQUEST_FAILED", *result.ErrorCode)
	assert.Equal(t, err.Error(), *result.ErrorMsg)
	assert.Equal(t, http.StatusUnauthorized, result.ResponseStatusCode)
	assert.Equal(t, map[string][]string(resp.Header), result.ResponseHeaders)
	assert.Equal(t, respBody, result.ResponseBody)
	assert.Equal(t, parsedBody, result.ResponseBodyParsed)
	assert.Equal(t, "https://example.com/login", result.RequestURL)
	assert.Equal(t, http.MethodPost, result.RequestMethod)
}
