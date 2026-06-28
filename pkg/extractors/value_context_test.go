package extractors_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValueResponseContext_JSONPathExtraction(t *testing.T) {
	value := map[string]any{
		"user": map[string]any{"name": "Ada", "id": "usr_1"},
		"list": []any{map[string]any{"k": "first"}},
	}
	ctx := extractors.NewValueResponseContext(value)

	got, err := extractors.JSONPathExtractor{Path: "$.user.name"}.Extract(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Ada", got)

	got, err = extractors.JSONPathExtractor{Path: "$.list[0].k"}.Extract(ctx)
	require.NoError(t, err)
	assert.Equal(t, "first", got)
}

func TestValueResponseContext_BodyExtraction(t *testing.T) {
	value := map[string]any{"status": "ok"}
	ctx := extractors.NewValueResponseContext(value)

	// The body extractor returns the parsed body, which is the underlying value.
	got, err := extractors.BodyExtractor{}.Extract(ctx)
	require.NoError(t, err)
	assert.Equal(t, value, got)
}

func TestValueResponseContext_RawBodyAndCapabilities(t *testing.T) {
	value := map[string]any{"a": float64(1)}
	ctx := extractors.NewValueResponseContext(value)

	// Raw body is the JSON marshalling of the value.
	pbr, ok := ctx.(extractors.ParsedBodyReader)
	require.True(t, ok)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(pbr.GetRawBody(), &decoded))
	assert.Equal(t, value, decoded)
	assert.Equal(t, value, pbr.GetParsedBody())

	assert.True(t, ctx.HasCapability("body"))
	assert.True(t, ctx.HasCapability("parsed_body"))
	assert.False(t, ctx.HasCapability("status"))
	assert.False(t, ctx.HasCapability("headers"))

	// Status reader reports 0; headers are empty.
	sr, ok := ctx.(extractors.StatusReader)
	require.True(t, ok)
	assert.Equal(t, 0, sr.GetStatus())

	ha, ok := ctx.(extractors.HeaderAccessor)
	require.True(t, ok)
	assert.Empty(t, ha.GetHeader("X-Anything"))
	assert.Empty(t, ha.Headers())
}
