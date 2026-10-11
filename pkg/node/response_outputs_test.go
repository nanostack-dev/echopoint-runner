package node_test

import (
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/assert"
)

func TestABinaryResponseBodyIsNotExposed(t *testing.T) {
	result := &node.RequestExecutionResult{
		ResponseStatusCode: 200,
		ResponseHeaders:    map[string][]string{"Content-Type": {"image/png"}},
		ResponseBody:       []byte{0x89, 'P', 'N', 'G', 0xff, 0xfe},
		ResponseBodyParsed: string([]byte{0x89, 'P', 'N', 'G', 0xff, 0xfe}),
	}

	outputs := result.ImplicitOutputs()

	assert.NotContains(t, outputs, node.ResponseOutputBody)
	assert.Equal(t, 200, outputs[node.ResponseOutputStatus])
}

func TestARequestWithoutAResponseExposesNoResponseOutputs(t *testing.T) {
	assert.Empty(t, (&node.RequestExecutionResult{}).ImplicitOutputs())
}

func TestResponseHeadersExposeTheFirstValueUnderTheCanonicalName(t *testing.T) {
	result := &node.RequestExecutionResult{
		ResponseStatusCode: 200,
		ResponseHeaders:    map[string][]string{"set-cookie": {"a=1", "b=2"}},
	}

	assert.Equal(t, map[string]any{"Set-Cookie": "a=1"},
		result.ImplicitOutputs()[node.ResponseOutputHeaders])
}
