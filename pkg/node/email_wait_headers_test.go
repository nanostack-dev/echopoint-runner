package node_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func headerCheck(name, operator, value string) string {
	return `{"extractor_type":"header","extractor_data":{"header_name":"` + name + `"},` +
		`"operator_type":"` + operator + `","operator_data":{"value":"` + value + `"}}`
}

func TestEmailWaitNode_HeaderExtractorReadsCcReplyToAndCustomHeaders(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{
		email("msg-1", "Hi").
			with("cc", "Boss <boss@acme.example>").
			with("headers", map[string][]string{
				"Reply-To":   {"support@acme.example"},
				"X-Campaign": {"welcome-1", "welcome-2"},
				"Subject":    {"stale subject"},
			}),
	})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`,
		headerCheck("Cc", "contains", "boss@acme.example"),
		headerCheck("Reply-To", "equals", "support@acme.example"),
		headerCheck("x-campaign", "equals", "welcome-1"),
		headerCheck("Subject", "equals", "Hi"),
	), url)
	require.NoError(t, err)
	assert.Equal(t, "Boss <boss@acme.example>", res.Outputs["cc"])
}

func TestEmailWaitNode_OldAPIWithoutCcAndHeaders(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Hi")})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`,
		headerCheck("From", "contains", "no-reply@acme.example"),
	), url)
	require.NoError(t, err)
	assert.Empty(t, res.Outputs["cc"])
	assert.Equal(t, false, res.Outputs["bcc"])
}

func TestEmailWaitNode_Bcc(t *testing.T) {
	const other = "someone@acme.example"
	cases := map[string]struct {
		to, cc, envelopeTo string
		want               bool
	}{
		"to only":    {to: inboxAddress, envelopeTo: inboxAddress, want: false},
		"in cc":      {to: other, cc: inboxAddress, envelopeTo: inboxAddress, want: false},
		"in neither": {to: other, cc: "boss@acme.example", envelopeTo: inboxAddress, want: true},
		"no cc":      {to: other, envelopeTo: inboxAddress, want: true},
		"display names": {
			to:         "Me <" + inboxAddress + ">, Other <" + other + ">",
			envelopeTo: inboxAddress,
			want:       false,
		},
		"display name in cc": {
			to:         other,
			cc:         "\"Ann, Lee\" <" + inboxAddress + ">",
			envelopeTo: inboxAddress,
			want:       false,
		},
		"uppercase header": {
			to:         "AANUYJABCDEFGHIJKLMSJD6S2G@IN.ECHOPOINT.DEV",
			envelopeTo: inboxAddress,
			want:       false,
		},
		"uppercase envelope": {
			to:         inboxAddress,
			envelopeTo: "AANUYJABCDEFGHIJKLMSJD6S2G@in.echopoint.dev",
			want:       false,
		},
		"plus tag differs": {
			to:         "aanuyjabcdefghijklmsjd6s2g+a@in.echopoint.dev",
			envelopeTo: inboxAddress,
			want:       true,
		},
		"unparsable header":  {to: "undisclosed <" + inboxAddress, envelopeTo: inboxAddress, want: false},
		"unparsable, absent": {to: "undisclosed <" + other, envelopeTo: inboxAddress, want: true},
		"empty envelope":     {to: other, envelopeTo: "", want: false},
		"missing to and cc":  {to: "", envelopeTo: inboxAddress, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			url, _ := serveInbox(t, []emailFixture{
				email("msg-1", "Hi").with("to", tc.to).with("cc", tc.cc).with("envelope_to", tc.envelopeTo),
			})
			res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`), url)
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Outputs["bcc"])
		})
	}
}

func TestEmailWaitNode_OutputsAlwaysHoldCcAndBcc(t *testing.T) {
	n := decodeEmailWait(t, emailWaitJSON(`{}`))
	assert.Contains(t, n.OutputSchema(), "cc")
	assert.Contains(t, n.OutputSchema(), "bcc")
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Hi")})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`), url)
	require.NoError(t, err)
	assert.Contains(t, res.Outputs, "cc")
	assert.Contains(t, res.Outputs, "bcc")
}
