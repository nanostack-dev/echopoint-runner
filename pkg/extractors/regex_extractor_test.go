package extractors_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
)

const codeEmailText = `Sign in to Acme

Your verification code is 482913. It expires in 10 minutes.
Reference: ORD-2026-0042`

func TestRegexExtractor_WholeMatchWithoutAGroup(t *testing.T) {
	value, err := extractors.RegexExtractor{Pattern: `\b\d{6}\b`}.Extract(htmlContext(codeEmailText))
	require.NoError(t, err)
	assert.Equal(t, "482913", value)
}

func TestRegexExtractor_FirstGroupByDefault(t *testing.T) {
	value, err := extractors.RegexExtractor{Pattern: `Reference: (ORD-[\d-]+)`}.Extract(htmlContext(codeEmailText))
	require.NoError(t, err)
	assert.Equal(t, "ORD-2026-0042", value)
}

func TestRegexExtractor_ExplicitGroup(t *testing.T) {
	value, err := extractors.RegexExtractor{Pattern: `(\w+) code is (\d+)`, Group: new(2)}.Extract(
		htmlContext(codeEmailText),
	)
	require.NoError(t, err)
	assert.Equal(t, "482913", value)

	whole, err := extractors.RegexExtractor{Pattern: `code is (\d+)`, Group: new(0)}.Extract(
		htmlContext(codeEmailText),
	)
	require.NoError(t, err)
	assert.Equal(t, "code is 482913", whole)
}

func TestRegexExtractor_Errors(t *testing.T) {
	_, err := extractors.RegexExtractor{Pattern: `(`}.Extract(htmlContext(codeEmailText))
	require.ErrorContains(t, err, "invalid regex")
	_, err = extractors.RegexExtractor{Pattern: `\d{8}`}.Extract(htmlContext(codeEmailText))
	require.ErrorContains(t, err, "did not match")
	_, err = extractors.RegexExtractor{Pattern: `(\d+)`, Group: new(3)}.Extract(htmlContext(codeEmailText))
	require.ErrorContains(t, err, "no group 3")
}

func TestRegexExtractor_DecodesFromTheRegistry(t *testing.T) {
	extractor, err := extractors.UnmarshalExtractor([]byte(`{"type":"regex","pattern":"(\\d+)","group":1}`))
	require.NoError(t, err)
	assert.Equal(t, extractors.RegexExtractor{Pattern: `(\d+)`, Group: new(1)}, extractor)
}
