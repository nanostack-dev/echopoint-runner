package node_test

import (
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resolveTemplate(t *testing.T, variables map[string]any, template string) any {
	t.Helper()
	resolved, err := node.NewTemplateResolver(variables).Resolve(template)
	require.NoError(t, err)
	return resolved
}

func TestTemplateResolver_ExactKeyWinsOverPathTraversal(t *testing.T) {
	variables := map[string]any{
		"account.id": "flat",
		"account":    map[string]any{"id": "nested"},
	}

	assert.Equal(t, "flat", resolveTemplate(t, variables, "{{account.id}}"))
}

func TestTemplateResolver_InterpolatesAFieldOfAnObject(t *testing.T) {
	variables := map[string]any{"account": map[string]any{"id": "a1"}}

	assert.Equal(t, "/accounts/a1", resolveTemplate(t, variables, "/accounts/{{account.id}}"))
}

func TestTemplateResolver_RawPathReturnsTheNestedValueWithItsType(t *testing.T) {
	variables := map[string]any{"account": map[string]any{"tags": []any{"x", "y"}}}

	assert.Equal(t, []any{"x", "y"}, resolveTemplate(t, variables, "{{{account.tags}}}"))
}

func TestTemplateResolver_IndexesIntoAnArray(t *testing.T) {
	variables := map[string]any{"items": []any{
		map[string]any{"name": "a"},
		map[string]any{"name": "b"},
	}}

	assert.Equal(t, "b", resolveTemplate(t, variables, "{{items.1.name}}"))
}

func TestTemplateResolver_LeavesTheTemplateWhenAPathSegmentIsMissing(t *testing.T) {
	variables := map[string]any{"account": map[string]any{"id": "a1"}, "items": []any{"a"}}

	assert.Equal(t, "{{account.name}}-{{items.5}}", resolveTemplate(t, variables, "{{account.name}}-{{items.5}}"))
}

func TestTemplateResolver_RawPathWithAMissingSegmentReturnsTheTemplate(t *testing.T) {
	variables := map[string]any{"account": map[string]any{"id": "a1"}}

	assert.Equal(t, "{{{account.name}}}", resolveTemplate(t, variables, "{{{account.name}}}"))
}

func TestTemplateResolver_WalksFromTheLongestDottedKey(t *testing.T) {
	variables := map[string]any{
		"list_accounts.accounts": []any{map[string]any{"id": "a1"}},
		"list_accounts":          map[string]any{"accounts": []any{map[string]any{"id": "shorter"}}},
	}

	assert.Equal(t, "a1", resolveTemplate(t, variables, "{{list_accounts.accounts.0.id}}"))
}

func TestTemplateResolver_IndexesIntoLoopResults(t *testing.T) {
	variables := map[string]any{"loop.results": []map[string]any{{"status": "ok"}}}

	assert.Equal(t, map[string]any{"status": "ok"}, resolveTemplate(t, variables, "{{{loop.results.0}}}"))
}

func TestTemplateResolver_WalksIntoAStringMap(t *testing.T) {
	resolver := node.NewTemplateResolver(map[string]any{
		"wait.headers": map[string]string{"content-type": "application/json"},
	})

	resolved, err := resolver.Resolve("{{wait.headers.content-type}}")

	require.NoError(t, err)
	assert.Equal(t, "application/json", resolved)
}

func TestResolveAssertions_WalksPathsIntoStepOutputsAndLoopItems(t *testing.T) {
	ctx := spi.ExecutionContext{
		AllOutputs: node.NewOutputView(map[string]map[string]any{
			"producer": {"account": map[string]any{"id": "a1"}},
		}),
		FlowInputs: map[string]any{"account": map[string]any{"id": "b2"}},
	}

	resolved, err := node.ResolveAssertions(ctx, []node.CompositeAssertion{
		{ExpectedValue: "{{producer.account.id}}"},
		{ExpectedValue: "{{account.id}}"},
	})

	require.NoError(t, err)
	assert.Equal(t, "a1", resolved[0].ExpectedValue)
	assert.Equal(t, "b2", resolved[1].ExpectedValue)
}

func TestResolveAssertions_PathRootedAtAStepNeverReadsAFlowInput(t *testing.T) {
	ctx := spi.ExecutionContext{
		AllOutputs: node.NewOutputView(map[string]map[string]any{
			"producer": {"name": "p"},
		}),
		FlowInputs: map[string]any{"producer": map[string]any{"id": "fallback"}},
	}

	_, err := node.ResolveAssertions(ctx, []node.CompositeAssertion{
		{ExpectedValue: "{{producer.id}}"},
	})

	require.Error(t, err)
	assert.Equal(t, "ASSERTION_FAILED", spi.ErrorCode(err))
}

func TestTemplateResolver_WalksIntoATypedSlice(t *testing.T) {
	assert.Equal(t, "alice", resolveTemplate(t,
		map[string]any{"names": []string{"alice"}}, "{{names.0}}"))
}

func TestTemplateResolver_InterpolatesALargeWholeNumberWithoutExponent(t *testing.T) {
	variables := map[string]any{"order": map[string]any{"id": float64(1234567)}}

	assert.Equal(t, "/orders/1234567", resolveTemplate(t, variables, "/orders/{{order.id}}"))
}

func TestTemplateResolver_InterpolatesASmallFractionWithoutExponent(t *testing.T) {
	variables := map[string]any{"rate": 0.0000001}

	assert.Equal(t, "rate=0.0000001", resolveTemplate(t, variables, "rate={{rate}}"))
}

func TestTemplateResolver_InterpolatesAnObjectAsJSON(t *testing.T) {
	variables := map[string]any{"filter": map[string]any{"tags": []any{"a", "b"}}}

	assert.Equal(t, `q={"tags":["a","b"]}`, resolveTemplate(t, variables, "q={{filter}}"))
}

func TestTemplateResolver_InterpolatesAnArrayAsJSON(t *testing.T) {
	variables := map[string]any{"ids": []any{float64(1000000), "x"}}

	assert.Equal(t, `ids=[1000000,"x"]`, resolveTemplate(t, variables, "ids={{ids}}"))
}
