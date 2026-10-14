package node_test

import (
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
)

func TestInferRequestNodeInputSchema_ATemplatedQueryParameterNameIsAnInput(t *testing.T) {
	var si node.SchemaInference

	inputs := si.InferRequestNodeInputSchema(node.RequestData{
		URL:         "https://example.com",
		QueryParams: map[string]any{"{{seed.key}}": "value"},
	})

	assert.Contains(t, inputs, "seed.key")
}
