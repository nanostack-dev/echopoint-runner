package extractors_test

import (
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
	response := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "John Doe",
			"age":  30,
		},
	}

	result, err := extractor.Extract(response)

	require.NoError(t, err)
	assert.Equal(t, "John Doe", result)
}

func TestJSONPathExtractor_Extract_NestedField(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.user.address.city"}
	response := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "John Doe",
			"address": map[string]interface{}{
				"city":    "New York",
				"country": "USA",
			},
		},
	}

	result, err := extractor.Extract(response)

	require.NoError(t, err)
	assert.Equal(t, "New York", result)
}

func TestJSONPathExtractor_Extract_ArrayElement(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.orders[0].id"}
	response := map[string]interface{}{
		"orders": []interface{}{
			map[string]interface{}{"id": "order-123", "total": 100},
			map[string]interface{}{"id": "order-456", "total": 200},
		},
	}

	result, err := extractor.Extract(response)

	require.NoError(t, err)
	assert.Equal(t, "order-123", result)
}

func TestJSONPathExtractor_Extract_NonexistentPath(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.nonexistent.field"}
	response := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "John Doe",
		},
	}

	result, err := extractor.Extract(response)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "did not match any nodes")
}

func TestJSONPathExtractor_Extract_InvalidJSON(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.user.name"}
	response := "invalid json"

	result, err := extractor.Extract(response)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse JSON")
}

func TestJSONPathExtractor_Extract_ArrayFilter(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.orders[?@.status=='active'].id"}
	response := map[string]interface{}{
		"orders": []interface{}{
			map[string]interface{}{"id": "order-123", "status": "active"},
			map[string]interface{}{"id": "order-456", "status": "completed"},
			map[string]interface{}{"id": "order-789", "status": "active"},
		},
	}

	result, err := extractor.Extract(response)

	require.NoError(t, err)
	// Should return array of matching ids
	resultSlice, ok := result.([]interface{})
	assert.True(t, ok, "result should be a slice")
	assert.Len(t, resultSlice, 2)
	assert.Contains(t, resultSlice, "order-123")
	assert.Contains(t, resultSlice, "order-789")
}

func TestJSONPathExtractor_Extract_JSONString(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.user.name"}
	jsonString := `{"user": {"name": "Jane Doe", "age": 25}}`

	result, err := extractor.Extract(jsonString)

	require.NoError(t, err)
	assert.Equal(t, "Jane Doe", result)
}

func TestJSONPathExtractor_Extract_JSONBytes(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$.user.age"}
	jsonBytes := []byte(`{"user": {"name": "Bob", "age": 35}}`)

	result, err := extractor.Extract(jsonBytes)

	require.NoError(t, err)
	// JSON numbers are unmarshaled as float64
	assert.InDelta(t, float64(35), result, 0.0001)
}

func TestJSONPathExtractor_Extract_InvalidPath(t *testing.T) {
	extractor := extractors.JSONPathExtractor{Path: "$[invalid"}
	response := map[string]interface{}{"key": "value"}

	result, err := extractor.Extract(response)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid JSONPath expression")
}
