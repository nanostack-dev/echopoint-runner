package engine_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func orderServer(t *testing.T, readBackID string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"ord_7Hk2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + readBackID + `"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func readBackFlow(t *testing.T, serverURL, expected string) *flow.Flow {
	t.Helper()
	definition := []byte(`{
		"name": "Order read-back",
		"version": "1.0",
		"nodes": [
			{
				"id": "create-order",
				"display_name": "Create Order",
				"type": "request",
				"data": {"method": "POST", "url": "` + serverURL + `/orders", "timeout": 1000},
				"outputs": [{"name": "orderId", "extractor": {"type": "jsonPath", "path": "$.id"}}]
			},
			{
				"id": "get-order",
				"display_name": "Get Order",
				"type": "request",
				"data": {"method": "GET", "url": "` + serverURL + `/orders/1", "timeout": 1000},
				"assertions": [{
					"extractor_type": "jsonPath",
					"extractor_data": {"path": "$.id"},
					"operator_type": "equals",
					"operator_data": {"value": "` + expected + `"}
				}]
			}
		],
		"edges": [{"id": "e1", "source": "create-order", "target": "get-order", "type": "success"}]
	}`)
	parsed, err := flow.ParseFromJSON(definition)
	require.NoError(t, err)
	return parsed
}

func TestFlowEngine_Execute_ARequestAssertionComparesAgainstAnEarlierOutput(t *testing.T) {
	server := orderServer(t, "ord_7Hk2")

	result, err := engine.ExecuteFlowDefinition(*readBackFlow(t, server.URL, "{{create-order.orderId}}"), nil, nil)

	require.NoError(t, err)
	require.True(t, result.Success)
	readBack := spi.MustAs[*node.RequestExecutionResult](result.ExecutionResults["get-order"])
	require.Len(t, readBack.AssertionResults, 1)
	assert.Equal(t, "ord_7Hk2", readBack.AssertionResults[0].Expected)
}

func TestFlowEngine_Execute_ARequestAssertionFailsWhenTheEarlierOutputDiffers(t *testing.T) {
	server := orderServer(t, "ord_other")

	result, _ := engine.ExecuteFlowDefinition(*readBackFlow(t, server.URL, "{{create-order.orderId}}"), nil, nil)

	require.False(t, result.Success)
	readBack := spi.MustAs[*node.RequestExecutionResult](result.ExecutionResults["get-order"])
	require.Len(t, readBack.AssertionResults, 1)
	assert.False(t, readBack.AssertionResults[0].Passed)
	assert.Equal(t, "ord_7Hk2", readBack.AssertionResults[0].Expected)
	assert.Equal(t, "ord_other", readBack.AssertionResults[0].Actual)
}

func TestParse_AnAssertionReferencingAnOutputNoStepProducesIsRejected(t *testing.T) {
	_, err := flow.ParseFromJSON([]byte(`{
		"name": "Dangling assertion",
		"version": "1.0",
		"nodes": [{
			"id": "get-order",
			"display_name": "Get Order",
			"type": "request",
			"data": {"method": "GET", "url": "https://example.test/orders/1", "timeout": 1000},
			"assertions": [{
				"extractor_type": "jsonPath",
				"extractor_data": {"path": "$.id"},
				"operator_type": "equals",
				"operator_data": {"value": "{{create-order.orderId}}"}
			}]
		}],
		"edges": []
	}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create-order")
}
