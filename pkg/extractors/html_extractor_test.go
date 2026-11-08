package extractors_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
)

// codeEmailHTML is the shape of a transactional code email: nested layout
// tables, inline styles, a preheader and a style block.
const codeEmailHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Your code</title>
<style>.code { font-size: 32px; letter-spacing: 4px }</style></head>
<body style="margin:0">
<span class="preheader" style="display:none">Use 482913 to sign in</span>
<table role="presentation" width="100%"><tr><td align="center">
  <table width="600"><tr><td>
    <h1>Sign in to Acme</h1>
    <p>Enter this code to finish signing in:</p>
    <p class="code" data-testid="otp"><strong>482913</strong></p>
    <p>The code expires in 10&nbsp;minutes.</p>
  </td></tr></table>
</td></tr></table>
<script>track()</script>
</body></html>`

// magicLinkEmailHTML has a button link with an encoded query string, the same
// link again as fallback text, a tracked link and a mailto.
const magicLinkEmailHTML = `<html><body>
<p>Hi Alice,</p>
<table><tr><td class="button">
<a href="https://staging.acme.example/verify?token=abc123&amp;email=alice%2Btag%40in.echopoint.dev" class="btn verify">Verify my email</a>
</td></tr></table>
<p>Or paste this link: <a href="https://staging.acme.example/verify?token=abc123&amp;email=alice%2Btag%40in.echopoint.dev">https://staging.acme.example/verify?token=abc123</a></p>
<p><a href="https://click.mailgun.example/c/eJx1">Unsubscribe</a> · <a href="mailto:help@acme.example">Help</a></p>
</body></html>`

// malformedEmailHTML is the markup real senders produce: unclosed tags, a
// stray closing tag, <br> soup and an attribute without quotes.
const malformedEmailHTML = `<div><p>Hello<br>your code is <b>  77 41 02 <br><br>
<p>Thanks<div class=footer>Acme</span>
<a href=https://acme.example/reset?t=x&y=1>Reset password`

func htmlContext(body string) extractors.ResponseContext {
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}}
	return extractors.NewResponseContext(resp, []byte(body), nil)
}

func TestHTMLExtractor_ReadsTheCodeOfACodeEmail(t *testing.T) {
	value, err := extractors.HTMLExtractor{Selector: `[data-testid="otp"]`}.Extract(htmlContext(codeEmailHTML))
	require.NoError(t, err)
	assert.Equal(t, "482913", value)
}

func TestHTMLExtractor_ReadsAnAttributeWithEntitiesDecoded(t *testing.T) {
	value, err := extractors.HTMLExtractor{Selector: "a.verify", Attribute: "href"}.Extract(
		htmlContext(magicLinkEmailHTML),
	)
	require.NoError(t, err)
	assert.Equal(t, "https://staging.acme.example/verify?token=abc123&email=alice%2Btag%40in.echopoint.dev", value)
}

func TestHTMLExtractor_AllListsEveryMatch(t *testing.T) {
	value, err := extractors.HTMLExtractor{Selector: "a", Attribute: "href", All: true}.Extract(
		htmlContext(magicLinkEmailHTML),
	)
	require.NoError(t, err)
	assert.Len(t, value, 4)
}

func TestHTMLExtractor_AllWithoutMatchIsAnEmptyList(t *testing.T) {
	value, err := extractors.HTMLExtractor{Selector: "img", All: true}.Extract(htmlContext(magicLinkEmailHTML))
	require.NoError(t, err)
	assert.Equal(t, []any{}, value)
}

func TestHTMLExtractor_SurvivesMalformedMarkup(t *testing.T) {
	ctx := htmlContext(malformedEmailHTML)
	text, err := extractors.HTMLExtractor{Selector: "b"}.Extract(ctx)
	require.NoError(t, err)
	assert.Equal(t, "77 41 02", text)
	href, err := extractors.HTMLExtractor{Selector: "a", Attribute: "href"}.Extract(ctx)
	require.NoError(t, err)
	assert.Equal(t, "https://acme.example/reset?t=x&y=1", href)
	footer, err := extractors.HTMLExtractor{Selector: ".footer"}.Extract(ctx)
	require.NoError(t, err)
	assert.Contains(t, footer, "Acme")
}

func TestHTMLExtractor_NoMatchIsAnError(t *testing.T) {
	_, err := extractors.HTMLExtractor{Selector: "a.missing"}.Extract(htmlContext(magicLinkEmailHTML))
	require.ErrorContains(t, err, "did not match")
}

func TestHTMLExtractor_MissingAttributeIsNoMatch(t *testing.T) {
	_, err := extractors.HTMLExtractor{Selector: "p", Attribute: "href"}.Extract(htmlContext(magicLinkEmailHTML))
	require.Error(t, err)
}

func TestHTMLExtractor_InvalidSelectorIsAnError(t *testing.T) {
	_, err := extractors.HTMLExtractor{Selector: "a[href"}.Extract(htmlContext(magicLinkEmailHTML))
	require.ErrorContains(t, err, "invalid CSS selector")
}

func TestHTMLExtractor_EmptyBodyIsAnError(t *testing.T) {
	_, err := extractors.HTMLExtractor{Selector: "a"}.Extract(htmlContext(""))
	require.Error(t, err)
}

func TestHTMLExtractor_DecodesFromTheRegistry(t *testing.T) {
	extractor, err := extractors.UnmarshalExtractor(
		[]byte(`{"type":"html","selector":"a.verify","attribute":"href"}`),
	)
	require.NoError(t, err)
	assert.Equal(t, extractors.HTMLExtractor{Selector: "a.verify", Attribute: "href"}, extractor)
}

func TestHTMLText_ReadsLikeAPerson(t *testing.T) {
	text := extractors.HTMLText(codeEmailHTML)
	assert.Contains(t, text, "Sign in to Acme\nEnter this code to finish signing in:\n482913\n")
	assert.Contains(t, text, "The code expires in 10 minutes.")
	assert.NotContains(t, text, "font-size")
	assert.NotContains(t, text, "track()")
	assert.NotContains(t, text, "Your code")
}

func TestHTMLText_SurvivesMalformedMarkup(t *testing.T) {
	text := extractors.HTMLText(malformedEmailHTML)
	assert.Contains(t, text, "Hello\nyour code is 77 41 02")
	assert.Contains(t, text, "Reset password")
}

func TestHTMLLinks_ListsHrefsInDocumentOrder(t *testing.T) {
	links := extractors.HTMLLinks(magicLinkEmailHTML)
	assert.Equal(t, []string{
		"https://staging.acme.example/verify?token=abc123&email=alice%2Btag%40in.echopoint.dev",
		"https://staging.acme.example/verify?token=abc123&email=alice%2Btag%40in.echopoint.dev",
		"https://click.mailgun.example/c/eJx1",
		"mailto:help@acme.example",
	}, links)
}
