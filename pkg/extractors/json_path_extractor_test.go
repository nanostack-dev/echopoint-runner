package extractors_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONPathExtractor_GetType(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.user.name"}
	assert.Equal(t, extractors.ExtractorTypeJSONPath, extractor.GetType())
}

func TestJSONPathExtractor_Extract_SimpleField(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.user.name"}
	data := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "John Doe",
			"age":  30,
		},
	}
	jsonBytes, _ := json.Marshal(data)
	ctx := extractors.NewResponseContext(&http.Response{}, jsonBytes, data)

	result, err := extractor.Extract(ctx)

	require.NoError(t, err)
	assert.Equal(t, "John Doe", result)
}

func TestJSONPathExtractor_Extract_NestedField(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.user.address.city"}
	data := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "John Doe",
			"address": map[string]interface{}{
				"city":    "New York",
				"country": "USA",
			},
		},
	}
	jsonBytes, _ := json.Marshal(data)
	ctx := extractors.NewResponseContext(&http.Response{}, jsonBytes, data)

	result, err := extractor.Extract(ctx)

	require.NoError(t, err)
	assert.Equal(t, "New York", result)
}

func TestJSONPathExtractor_Extract_ArrayElement(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.orders[0].id"}
	data := map[string]interface{}{
		"orders": []interface{}{
			map[string]interface{}{"id": "order-123", "total": 100},
			map[string]interface{}{"id": "order-456", "total": 200},
		},
	}
	jsonBytes, _ := json.Marshal(data)
	ctx := extractors.NewResponseContext(&http.Response{}, jsonBytes, data)

	result, err := extractor.Extract(ctx)

	require.NoError(t, err)
	assert.Equal(t, "order-123", result)
}

func TestJSONPathExtractor_Extract_NonexistentPath(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.nonexistent.field"}
	data := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "John Doe",
		},
	}
	jsonBytes, _ := json.Marshal(data)
	ctx := extractors.NewResponseContext(&http.Response{}, jsonBytes, data)

	result, err := extractor.Extract(ctx)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "did not match any nodes")
}

func TestJSONPathExtractor_Extract_ArrayFilter(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.orders[?@.status=='active'].id"}
	data := map[string]interface{}{
		"orders": []interface{}{
			map[string]interface{}{"id": "order-123", "status": "active"},
			map[string]interface{}{"id": "order-456", "status": "completed"},
			map[string]interface{}{"id": "order-789", "status": "active"},
		},
	}
	jsonBytes, _ := json.Marshal(data)
	ctx := extractors.NewResponseContext(&http.Response{}, jsonBytes, data)

	result, err := extractor.Extract(ctx)

	require.NoError(t, err)
	// Should return array of matching ids
	resultSlice, ok := result.([]interface{})
	assert.True(t, ok, "result should be a slice")
	assert.Len(t, resultSlice, 2)
	assert.Contains(t, resultSlice, "order-123")
	assert.Contains(t, resultSlice, "order-789")
}

func TestJSONPathExtractor_Extract_InvalidPath(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$[invalid"}
	data := map[string]interface{}{"key": "value"}
	jsonBytes, _ := json.Marshal(data)
	ctx := extractors.NewResponseContext(&http.Response{}, jsonBytes, data)

	result, err := extractor.Extract(ctx)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid JSONPath expression")
}

func TestJSONPathExtractor_Extract_MissingParsedBody(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.user.name"}
	// Create context with nil parsed body
	ctx := extractors.NewResponseContext(&http.Response{}, nil, nil)

	result, err := extractor.Extract(ctx)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestJSONPathExtractor_Extract_MultipleResults(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.items[*].id"}
	data := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"id": "item-1"},
			map[string]interface{}{"id": "item-2"},
			map[string]interface{}{"id": "item-3"},
		},
	}
	jsonBytes, _ := json.Marshal(data)
	ctx := extractors.NewResponseContext(&http.Response{}, jsonBytes, data)

	result, err := extractor.Extract(ctx)

	require.NoError(t, err)
	resultSlice, ok := result.([]interface{})
	require.True(t, ok, "result should be a slice")
	assert.Len(t, resultSlice, 3)
}
