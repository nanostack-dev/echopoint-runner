package node_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const secretOutputsJSON = `"outputs":[` +
	`{"name":"code","secret":true,"extractor":{"type":"json_path","path":"$.code"}},` +
	`{"name":"link","secret":true,"extractor":{"type":"json_path","path":"$.first_link"}}]`

func secretEmailWait(data string, assertions ...string) []byte {
	return []byte(`{"id":"verify","display_name":"Verify","type":"email_wait","data":` + data +
		`,"assertions":[` + strings.Join(assertions, ",") + `],` + secretOutputsJSON + `}`)
}

// reportedEmailWait runs a wait under a run's redaction and returns its
// result as the report carries it, with the error of the wait.
func reportedEmailWait(t *testing.T, raw []byte, url string) (map[string]any, string, error) {
	t.Helper()
	redactor := redact.New(map[string]any{"api_key": "an-input-secret-value"}, []string{"api_key"})
	ctx := redact.WithLearner(spi.WithJobToken(context.Background(), "tok"), redactor)
	res, waitErr := decodeEmailWait(t, raw).Execute(spi.ExecutionContext{
		Ctx:        ctx,
		FlowInputs: map[string]any{"inbox.messages_url": url},
	})
	encoded, err := json.Marshal(res)
	require.NoError(t, err)
	var tree map[string]any
	require.NoError(t, json.Unmarshal(encoded, &tree))
	reported := redactor.Map(tree)
	masked, err := json.Marshal(reported)
	require.NoError(t, err)
	return reported, string(masked), waitErr
}

func TestEmailWaitNode_AShortSecretCodeLeavesNoBodyInTheReport(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{
		email("msg-1", "Your PIN").with("text", "Your PIN is 4829").with("html", "<p>Your PIN is <b>4829</b></p>"),
	})
	data := `{"timeout_ms":2000,"code_pattern":"\\b\\d{4}\\b"}`
	reported, masked, err := reportedEmailWait(t, secretEmailWait(data), url)
	require.NoError(t, err)
	assert.NotContains(t, masked, "4829")
	outputs, ok := reported["outputs"].(map[string]any)
	require.True(t, ok)
	for _, name := range []string{"code", "text", "html", "subject", "links", "first_link"} {
		assert.Equal(t, redact.Mask, outputs[name], name)
	}
	assert.Equal(t, "msg-1", outputs["id"], "what cannot carry the code stays")
}

func TestEmailWaitNode_ASecretLinkIsMaskedInTheHTMLSource(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Code").with("html", codeHTML)})
	_, masked, err := reportedEmailWait(t, secretEmailWait(`{"timeout_ms":2000}`), url)
	require.NoError(t, err)
	for _, form := range []string{"t=abc&u=1", `t=abc\u0026u=1`, "t=abc&amp;u=1", `t=abc\u0026amp;u=1`} {
		assert.NotContains(t, masked, form)
	}
	assert.NotContains(t, masked, "482913")
}

func TestEmailWaitNode_ATimeoutMasksTheSecretsOfUnmatchedMessages(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{
		email("msg-1", "Sign in").with("html", codeHTML),
	})
	_, masked, err := reportedEmailWait(t, secretEmailWait(`{"timeout_ms":400}`,
		jsonPathCheck("$.subject", "contains", "Welcome"),
		jsonPathCheck("$.code", "equals", "000000"),
		jsonPathCheck("$.first_link", "contains", "nowhere"),
	), url)
	require.Error(t, err)
	assert.Contains(t, masked, "closest", "the closest message is still reported")
	assert.NotContains(t, masked, "482913")
	assert.NotContains(t, masked, "t=abc&u=1")
	assert.NotContains(t, masked, `t=abc\u0026u=1`)
}

func TestEmailWaitExpect_ClosestActualsAreCapped(t *testing.T) {
	long := strings.Repeat("a", 10<<10)
	data := `{"timeout_ms":400,"expect":[{"name":"verification","assertions":[` +
		jsonPathCheck("$.subject", "contains", "Verify") + "," +
		jsonPathCheck("$.text", "equals", "short") + `]}]}`
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Verify").with("text", long)})
	res, err := runEmailWait(t, emailWaitJSON(data), url)
	require.Error(t, err)
	require.NotNil(t, res.Expectations[0].Closest)
	actual, ok := res.Expectations[0].Closest.Checks[1].Actual.(string)
	require.True(t, ok)
	assert.LessOrEqual(t, len(actual), 4<<10+len("…"))
}

func TestEmailWaitNode_TheCodeIgnoresDigitsInScriptAndStyle(t *testing.T) {
	html := `<html><head><style>.c111111{width:222222px}</style><script>var n=333333</script></head>` +
		`<body><p>Your code is 482913</p></body></html>`
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Code").with("html", html)})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`), url)
	require.NoError(t, err)
	assert.Equal(t, "482913", res.Outputs["code"])
}
