package node_test

import (
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSetVariableNode(vars map[string]any) *node.SetVariableNode {
	return &node.SetVariableNode{
		BaseNode: node.BaseNode{
			ID:          "set1",
			DisplayName: "Set Variables",
			NodeType:    spi.KindSetVariable,
		},
		Data: node.SetVariableData{Variables: vars},
	}
}

func TestSetVariableNode_StringConcat(t *testing.T) {
	n := newSetVariableNode(map[string]any{
		"label": "{{u.id}}-{{u.name}}",
	})

	result, err := n.Execute(spi.ExecutionContext{
		Inputs: map[string]any{"u.id": 1, "u.name": "a"},
	})
	require.NoError(t, err)
	require.NoError(t, result.GetError())
	assert.Equal(t, "1-a", result.GetOutputs()["label"])
	assert.Equal(t, spi.KindSetVariable, result.GetNodeType())
}

func TestSetVariableNode_RawStructuredPassthrough(t *testing.T) {
	obj := map[string]any{"nested": []any{"x", "y"}, "count": 2}
	n := newSetVariableNode(map[string]any{
		"payload": "{{{u.obj}}}",
	})

	result, err := n.Execute(spi.ExecutionContext{
		Inputs: map[string]any{"u.obj": obj},
	})
	require.NoError(t, err)
	// Raw triple-brace reference returns the structured value unchanged.
	assert.Equal(t, obj, result.GetOutputs()["payload"])
}

func TestSetVariableNode_ScalarPassthrough(t *testing.T) {
	n := newSetVariableNode(map[string]any{
		"num":  42,
		"flag": true,
	})

	result, err := n.Execute(spi.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)
	assert.Equal(t, 42, result.GetOutputs()["num"])
	assert.Equal(t, true, result.GetOutputs()["flag"])
}

func TestSetVariableNode_NestedObjectOfTemplates(t *testing.T) {
	n := newSetVariableNode(map[string]any{
		"request": map[string]any{
			"path":  "/users/{{create.id}}",
			"items": []any{"{{create.id}}", "static"},
			"meta": map[string]any{
				"name": "{{create.name}}",
			},
		},
	})

	result, err := n.Execute(spi.ExecutionContext{
		Inputs: map[string]any{"create.id": 7, "create.name": "neo"},
	})
	require.NoError(t, err)

	want := map[string]any{
		"path":  "/users/7",
		"items": []any{"7", "static"},
		"meta": map[string]any{
			"name": "neo",
		},
	}
	assert.Equal(t, want, result.GetOutputs()["request"])
}

func TestSetVariableNode_UnknownVariableLeftLiteral(t *testing.T) {
	n := newSetVariableNode(map[string]any{
		"value": "{{missing}}",
	})

	result, err := n.Execute(spi.ExecutionContext{Inputs: map[string]any{}})
	require.NoError(t, err)
	// Unresolved references are left intact rather than blanked.
	assert.Equal(t, "{{missing}}", result.GetOutputs()["value"])
}

func TestSetVariableNode_Schemas(t *testing.T) {
	n := newSetVariableNode(map[string]any{
		"greeting": "{{u.name}} from {{org.id}}",
		"id":       "{{u.id}}",
		"static":   123,
	})

	// InputSchema is the sorted set of referenced template variables.
	assert.Equal(t, []string{"org.id", "u.id", "u.name"}, n.InputSchema())
	// OutputSchema is the sorted set of produced variable names.
	assert.Equal(t, []string{"greeting", "id", "static"}, n.OutputSchema())
}

func TestSetVariableNode_DecodeViaUnmarshalNode(t *testing.T) {
	raw := []byte(`{
		"id": "sv1",
		"type": "set_variable",
		"display_name": "Build Payload",
		"data": {
			"variables": {
				"label": "{{u.id}}-{{u.name}}",
				"payload": "{{{u.obj}}}",
				"count": 3
			}
		}
	}`)

	anyNode, err := node.UnmarshalNode(raw)
	require.NoError(t, err)
	require.Equal(t, spi.KindSetVariable, anyNode.GetType())
	require.Equal(t, spi.RunWhenOnSuccess, anyNode.GetRunWhen())

	sv, ok := node.AsSetVariableNode(anyNode)
	require.True(t, ok)
	assert.Equal(t, "{{u.id}}-{{u.name}}", sv.GetData().Variables["label"])

	result, err := sv.Execute(spi.ExecutionContext{
		Inputs: map[string]any{
			"u.id":   9,
			"u.name": "trinity",
			"u.obj":  map[string]any{"role": "admin"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "9-trinity", result.GetOutputs()["label"])
	assert.Equal(t, map[string]any{"role": "admin"}, result.GetOutputs()["payload"])
	// JSON numbers decode to float64 and pass through unchanged.
	assert.InDelta(t, float64(3), result.GetOutputs()["count"], 0)
}
