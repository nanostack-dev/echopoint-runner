package node_test

import (
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
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
