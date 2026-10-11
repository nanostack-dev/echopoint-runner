package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type mapModuleResolver map[string]spi.ResolvedModuleFlow

func (r mapModuleResolver) ResolveFlow(flowID string) (spi.ResolvedModuleFlow, bool) {
	resolved, ok := r[flowID]
	return resolved, ok
}

// Input names may contain commas: ["account", "z"] and ["account,z"] are
// different inputs and must not share a cached validation.
func TestResolveModuleFlow_CacheKeepsInputListsWithCommasApart(t *testing.T) {
	resolver := mapModuleResolver{"child": {FlowDefinition: []byte(`{
		"name": "child", "version": "1.0",
		"nodes": [{"id": "pick", "type": "set_variable",
			"data": {"variables": {"first_id": "{{account.id}}"}}}],
		"edges": []
	}`)}}
	parsed := make(map[string]flow.Flow)

	_, _, err := resolveModuleFlow("child", resolver, parsed, []string{"account", "z"})
	require.NoError(t, err)

	_, _, err = resolveModuleFlow("child", resolver, parsed, []string{"account,z"})
	require.Error(t, err, "a child without an account input must fail validation")
}
