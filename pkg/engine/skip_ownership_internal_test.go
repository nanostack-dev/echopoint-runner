package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

func consumerOf(t *testing.T, ref string) node.AnyNode {
	t.Helper()
	consumer, err := node.UnmarshalNode([]byte(`{"id": "consumer", "type": "set_variable",
		"data": {"variables": {"v": "{{` + ref + `}}"}}}`))
	require.NoError(t, err)
	return consumer
}

// A skipped step that published outputs owns references rooted at its id, as
// input assembly reads them from it: the consumer depends on that step.
func TestInputsReferenceSkippedNode_SkippedStepWithOutputsOwnsTheReference(t *testing.T) {
	state := &executionState{
		allOutputs:   map[string]map[string]any{"account": {}, "": {"account.id": "a1"}},
		skippedNodes: map[string]bool{"account": true},
	}

	assert.True(t, (&FlowEngine{}).inputsReferenceSkippedNode(consumerOf(t, "account.id"), state))
}

// A skipped step that published nothing leaves a path rooted at a flow input of
// the same name to that input.
func TestInputsReferenceSkippedNode_FlowInputOwnsAPathWhenTheStepPublishedNothing(t *testing.T) {
	state := &executionState{
		allOutputs:   map[string]map[string]any{"": {"account": map[string]any{"id": "a1"}}},
		skippedNodes: map[string]bool{"account": true},
	}

	assert.False(t, (&FlowEngine{}).inputsReferenceSkippedNode(consumerOf(t, "account.id"), state))
}
