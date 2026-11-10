package node_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const (
	inboxAddress = "aanuyjabcdefghijklmsjd6s2g@in.echopoint.dev"
	codeHTML     = `<html><head><style>p{color:red}</style></head><body>` +
		`<h1>Sign in</h1><p>Your code is <b>482913</b>.</p>` +
		`<p><a href="https://staging.acme.example/verify?t=abc&amp;u=1">Verify</a></p>` +
		`<p><a href="https://staging.acme.example/verify?t=abc&amp;u=1">https://staging.acme.example/verify?t=abc</a>` +
		` <a href="https://click.tracker.example/c/1">Unsubscribe</a> <a href="mailto:help@acme.example">Help</a></p>` +
		`</body></html>`
)

type emailFixture map[string]any

func email(id, subject string) emailFixture {
	return emailFixture{
		"id":            id,
		"envelope_from": "bounces@acme.example",
		"envelope_to":   inboxAddress,
		"from":          "Acme <no-reply@acme.example>",
		"to":            inboxAddress,
		"subject":       subject,
		"message_id":    "<" + id + "@acme.example>",
		"text":          nil,
		"html":          nil,
		"attachments":   []any{},
		"raw_size":      1024,
		"received_at":   time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
}

func (f emailFixture) with(key string, value any) emailFixture {
	f[key] = value
	return f
}

func inboxJSON(items ...emailFixture) []byte {
	if items == nil {
		items = []emailFixture{}
	}
	raw, err := json.Marshal(map[string]any{"items": items, "count": len(items), "total": len(items)})
	if err != nil {
		panic(err)
	}
	return raw
}

func emailWaitJSON(data string, assertions ...string) []byte {
	list := "[" + strings.Join(assertions, ",") + "]"
	return []byte(`{"id":"verify","display_name":"Verification email","type":"email_wait",` +
		`"assertions":` + list + `,"data":` + data + `}`)
}

func jsonPathCheck(path, operator, value string) string {
	return `{"extractor_type":"json_path","extractor_data":{"path":"` + path + `"},` +
		`"operator_type":"` + operator + `","operator_data":{"value":"` + value + `"}}`
}

func decodeEmailWait(t *testing.T, raw []byte) *node.EmailWaitNode {
	t.Helper()
	n, err := node.UnmarshalNode(raw)
	require.NoError(t, err)
	wait, ok := node.AsEmailWaitNode(n)
	require.True(t, ok, "got %T", n)
	return wait
}

func serveInbox(t *testing.T, batches ...[]emailFixture) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Job-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		call := int(calls.Add(1))
		batch := batches[min(call, len(batches))-1]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(inboxJSON(batch...))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

func runEmailWait(t *testing.T, raw []byte, url string) (*node.EmailWaitExecutionResult, error) {
	t.Helper()
	res, err := decodeEmailWait(t, raw).Execute(spi.ExecutionContext{
		Ctx:        spi.WithJobToken(context.Background(), "tok"),
		FlowInputs: map[string]any{"inbox.messages_url": url},
	})
	waitRes, ok := spi.As[*node.EmailWaitExecutionResult](res)
	require.True(t, ok, "got %T", res)
	return waitRes, err
}

func TestEmailWaitNode_DecodeDefaults(t *testing.T) {
	n := decodeEmailWait(t, emailWaitJSON(`{}`))
	assert.Equal(t, spi.KindEmailWait, n.GetType())
	assert.Equal(t, spi.RunWhenOnSuccess, n.GetRunWhen())
	assert.Equal(t, 60000, node.EmailWaitTimeoutMsForTest(n))
	assert.Contains(t, n.OutputSchema(), "first_link")
}

func TestEmailWaitNode_RejectsABadConfiguration(t *testing.T) {
	for name, data := range map[string]string{
		"timeout past the address lifetime": `{"timeout_ms":7200001}`,
		"settle as long as the timeout":     `{"timeout_ms":1000,"settle_ms":1000,"expect":[{"name":"a","assertions":[` + jsonPathCheck("$.subject", "contains", "x") + `]}]}`,
		"bad link pattern":                  `{"link_pattern":"("}`,
		"bad code pattern":                  `{"code_pattern":"[a-"}`,
		"unnamed expected event":            `{"expect":[{"assertions":[` + jsonPathCheck("$.subject", "contains", "x") + `]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := node.UnmarshalNode(emailWaitJSON(data))
			require.Error(t, err)
			assert.Equal(t, "EMAIL_WAIT_FAILED", spi.ErrorCode(err))
		})
	}
	_, err := node.UnmarshalNode(emailWaitJSON(`{"timeout_ms":7200000}`))
	require.NoError(t, err, "the address lifetime itself is allowed")
}

func TestEmailWaitNode_MatchesAndPublishesTheMessage(t *testing.T) {
	url, calls := serveInbox(t,
		nil,
		[]emailFixture{email("msg-1", "Welcome").with("text", "Hello")},
		[]emailFixture{
			email("msg-1", "Welcome").with("text", "Hello"),
			email("msg-2", "Verify your email").with("html", codeHTML).with("text", "Your code is 482913.\n"+
				"Verify: https://staging.acme.example/verify?t=abc&u=1"),
		},
	)
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":3000}`,
		jsonPathCheck("$.subject", "contains", "Verify")), url)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls.Load(), int32(3))
	out := res.Outputs
	assert.Equal(t, "msg-2", out["id"])
	assert.Equal(t, "Verify your email", out["subject"])
	assert.Equal(t, "Acme <no-reply@acme.example>", out["from"])
	assert.Equal(t, inboxAddress, out["envelope_to"])
	assert.Equal(t, "482913", out["code"])
	assert.Equal(t, []any{
		"https://staging.acme.example/verify?t=abc&u=1",
		"https://click.tracker.example/c/1",
	}, out["links"], "HTML hrefs, deduplicated, without mailto")
	assert.Equal(t, "https://staging.acme.example/verify?t=abc&u=1", out["first_link"])
	assert.Equal(t, codeHTML, out["html"])
	assert.Equal(t, "2026-11-08T12:00:00Z", out["received_at"])
	assert.NotNil(t, res.AssertionContext())
}

func TestEmailWaitNode_HTMLOnlyMessageFallsBackToItsText(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Your code").with("html", codeHTML)})
	res, err := runEmailWait(t, emailWaitJSON(
		`{"timeout_ms":2000}`,
		`{"extractor_type":"body","extractor_data":{},"operator_type":"contains","operator_data":{"value":"Your code is 482913"}}`,
	), url)
	require.NoError(t, err)
	assert.Equal(t, "Sign in\nYour code is 482913.\nVerify\nhttps://staging.acme.example/verify?t=abc Unsubscribe Help",
		res.Outputs["text"])
	assert.Equal(t, "482913", res.Outputs["code"])
}

func TestEmailWaitNode_TextOnlyMessageReadsLinksFromItsText(t *testing.T) {
	text := "Reset your password: https://acme.example/reset?token=x1.\n" +
		"Questions? Write to mailto:help@acme.example or see (https://acme.example/help)."
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Reset").with("text", text)})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000,"link_pattern":"/reset"}`), url)
	require.NoError(t, err)
	assert.Equal(t, []any{"https://acme.example/reset?token=x1"}, res.Outputs["links"])
	assert.Empty(t, res.Outputs["code"], "no six digits: code is empty, never missing")
	assert.Empty(t, res.Outputs["html"])
}

func TestEmailWaitNode_CodePatternUsesItsFirstGroup(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Code").with("text", "Your code: AB-12CD. Ref 123456")})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000,"code_pattern":"code: ([A-Z0-9-]+)"}`), url)
	require.NoError(t, err)
	assert.Equal(t, "AB-12CD", res.Outputs["code"])
}

func TestEmailWaitNode_MatchesThePlusTagRecipient(t *testing.T) {
	alice := "aanuyjabcdefghijklmsjd6s2g+alice@in.echopoint.dev"
	bob := "aanuyjabcdefghijklmsjd6s2g+bob@in.echopoint.dev"
	url, _ := serveInbox(t, []emailFixture{
		email("msg-bob", "Verify").with("envelope_to", bob).with("text", "code 111111"),
		email("msg-alice", "Verify").with("envelope_to", alice).with("text", "code 222222"),
	})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`,
		jsonPathCheck("$.envelope_to", "contains", "+alice@")), url)
	require.NoError(t, err)
	assert.Equal(t, "msg-alice", res.Outputs["id"])
	assert.Equal(t, "222222", res.Outputs["code"])
}

func TestEmailWaitNode_ReadsHeaders(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Hello").with("text", "x")})
	_, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`,
		`{"extractor_type":"header","extractor_data":{"header_name":"From"},"operator_type":"contains",`+
			`"operator_data":{"value":"no-reply@acme.example"}}`), url)
	require.NoError(t, err)
}

func TestEmailWaitNode_WithoutAssertionTheFirstMessageMatches(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "First"), email("msg-2", "Second")})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`), url)
	require.NoError(t, err)
	assert.Equal(t, "msg-1", res.Outputs["id"])
}

func TestEmailWaitNode_TimeoutReportsTheClosestMessage(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{
		email("msg-1", "Newsletter").with("text", "news"),
		email("msg-2", "Verify your email").with("text", "Your code is 12345"),
	})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":400}`,
		jsonPathCheck("$.subject", "contains", "Verify"),
		jsonPathCheck("$.code", "not_empty", ""),
	), url)
	require.Error(t, err)
	assert.Equal(t, "EMAIL_WAIT_TIMEOUT", spi.ErrorCode(err))
	require.NotNil(t, res.Closest)
	assert.Equal(t, "msg-2", res.Closest.RequestID)
	require.Len(t, res.Closest.Checks, 2)
	assert.True(t, res.Closest.Checks[0].Passed)
	assert.False(t, res.Closest.Checks[1].Passed)
	assert.Equal(t, "Your code is 12345", res.Closest.Text)
	require.Len(t, res.RecentMessages, 2)
	assert.Equal(t, "Newsletter", res.RecentMessages[0]["subject"])
	assert.NotContains(t, res.RecentMessages[0], "text", "a failure never reports bodies")
	checks, ok := res.RecentMessages[0]["checks"].([]node.ExpectationCheck)
	require.True(t, ok, "each recent message carries its checks")
	require.Len(t, checks, 2)
	assert.False(t, checks[0].Passed)
	assert.Equal(t, "Newsletter", checks[0].Actual)
	assert.Nil(t, res.AssertionContext())
}

func TestEmailWaitNode_FailuresAreFatal(t *testing.T) {
	for name, status := range map[string]int{"token rejected": http.StatusUnauthorized, "run ended": http.StatusNotFound} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)
			start := time.Now()
			_, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":5000}`), srv.URL)
			require.Error(t, err)
			assert.Equal(t, "EMAIL_WAIT_FAILED", spi.ErrorCode(err))
			assert.Less(t, time.Since(start), 2*time.Second)
		})
	}
}

func TestEmailWaitNode_RequiresTheInboxAndAJobToken(t *testing.T) {
	_, err := decodeEmailWait(t, emailWaitJSON(`{}`)).Execute(spi.ExecutionContext{Ctx: context.Background()})
	require.Error(t, err)
	assert.Equal(t, "EMAIL_WAIT_FAILED", spi.ErrorCode(err))
}

func TestEmailWaitNode_DeclaredOutputsReadTheMatchedMessage(t *testing.T) {
	raw := []byte(`{"id":"verify","display_name":"Verify","type":"email_wait","data":{"timeout_ms":2000},` +
		`"assertions":[],"outputs":[` +
		`{"name":"button","extractor":{"type":"html","selector":"a","attribute":"href"}},` +
		`{"name":"code","secret":true,"extractor":{"type":"json_path","path":"$.code"}},` +
		`{"name":"digits","extractor":{"type":"regex","pattern":"code is (\\d+)"}}]}`)
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Code").with("html", codeHTML)})
	wait := decodeEmailWait(t, raw)
	assert.Contains(t, wait.OutputSchema(), "button")
	res, err := runEmailWait(t, raw, url)
	require.NoError(t, err)
	produced, err := node.ExtractOutputs(wait.GetOutputs(), res.AssertionContext())
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"button": "https://staging.acme.example/verify?t=abc&u=1",
		"code":   "482913",
		"digits": "482913",
	}, produced)
}

func TestEmailWaitExpect_JudgesEveryGroup(t *testing.T) {
	data := `{"timeout_ms":3000,"settle_ms":300,"expect":[` +
		`{"name":"verification","min":1,"max":1,"assertions":[` + jsonPathCheck("$.subject", "contains", "Verify") + `]},` +
		`{"name":"welcome","assertions":[` + jsonPathCheck("$.subject", "contains", "Welcome") + `]}]}`
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Verify your email"), email("msg-2", "Welcome aboard")})
	res, err := runEmailWait(t, emailWaitJSON(data), url)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Outputs["received"])
	require.Len(t, res.Expectations, 2)
	assert.Equal(t, []string{"msg-1"}, res.Expectations[0].RequestIDs)
}

func TestEmailWaitExpect_FailsOnADuplicate(t *testing.T) {
	data := `{"timeout_ms":3000,"expect":[{"name":"verification","max":1,"assertions":[` +
		jsonPathCheck("$.subject", "contains", "Verify") + `]}]}`
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Verify"), email("msg-2", "Verify again")})
	res, err := runEmailWait(t, emailWaitJSON(data), url)
	require.Error(t, err)
	assert.Equal(t, "EMAIL_WAIT_EXPECTATIONS_FAILED", spi.ErrorCode(err))
	assert.Equal(t, "2 arrived", res.Expectations[0].Problem)
}

func TestEmailWaitNode_SuccessReportsTheMessagesBeforeTheMatch(t *testing.T) {
	url, _ := serveInbox(t, []emailFixture{
		email("msg-1", "Welcome").with("text", "hi"),
		email("msg-2", "Verify").with("text", "code 123456"),
		email("msg-3", "Later").with("text", "bye"),
	})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":2000}`,
		jsonPathCheck("$.subject", "contains", "Verify")), url)
	require.NoError(t, err)
	require.Len(t, res.RecentMessages, 1, "only what arrived before the match")
	assert.Equal(t, "msg-1", res.RecentMessages[0]["id"])
	checks, ok := res.RecentMessages[0]["checks"].([]node.ExpectationCheck)
	require.True(t, ok)
	assert.False(t, checks[0].Passed)
	assert.Nil(t, res.Closest)
}

func TestEmailWaitNode_ClosestTextAndActualsAreCapped(t *testing.T) {
	long := strings.Repeat("é", 3000) // 6000 bytes
	url, _ := serveInbox(t, []emailFixture{email("msg-1", "Hello").with("text", long)})
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":400}`,
		`{"extractor_type":"body","extractor_data":{},"operator_type":"contains","operator_data":{"value":"Verify"}}`,
	), url)
	require.Error(t, err)
	require.NotNil(t, res.Closest)
	assert.LessOrEqual(t, len(res.Closest.Text), 4096+len("…"))
	assert.True(t, strings.HasSuffix(res.Closest.Text, "…"))
	assert.True(t, utf8.ValidString(res.Closest.Text))
	actual, _ := res.Closest.Checks[0].Actual.(string)
	assert.LessOrEqual(t, len(actual), 4096+len("…"))
}

func TestEmailWaitNode_WithoutAssertionReportsNoChecks(t *testing.T) {
	url, _ := serveInbox(t, nil)
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":300}`), url)
	require.Error(t, err)
	assert.Nil(t, res.Closest)
	assert.Empty(t, res.RecentMessages)
}

func TestEmailWaitNode_APollReadsOnlyWhatArrivedAfterTheLastMessage(t *testing.T) {
	stored := []emailFixture{
		email("msg-1", "Welcome").with("text", "hi"),
		email("msg-2", "Newsletter").with("text", "news"),
		email("msg-3", "Verify").with("text", "code 123456"),
	}
	var cursors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		cursors = append(cursors, after)
		// One more message is stored between two polls.
		arrived := stored[:min(len(cursors), len(stored))]
		from := 0
		for i, message := range arrived {
			if message["id"] == after {
				from = i + 1
			}
		}
		_, _ = w.Write(inboxJSON(arrived[from:]...))
	}))
	t.Cleanup(srv.Close)

	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":5000}`,
		jsonPathCheck("$.subject", "contains", "Verify")), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, []string{"", "msg-1", "msg-2"}, cursors, "each poll continues after the last message read")
	require.Len(t, res.RecentMessages, 2, "messages of earlier polls are kept")
	assert.Equal(t, "msg-1", res.RecentMessages[0]["id"])
	assert.Equal(t, "msg-2", res.RecentMessages[1]["id"])
}

func TestEmailWaitNode_AnAPIWithoutTheCursorStillWorks(t *testing.T) {
	url, calls := serveInbox(t,
		[]emailFixture{email("msg-1", "Welcome").with("text", "hi")},
		[]emailFixture{
			email("msg-1", "Welcome").with("text", "hi"),
			email("msg-2", "Verify").with("text", "code 123456"),
		},
	)
	res, err := runEmailWait(t, emailWaitJSON(`{"timeout_ms":5000}`,
		jsonPathCheck("$.subject", "contains", "Verify")), url)
	require.NoError(t, err)
	assert.EqualValues(t, 2, calls.Load())
	require.Len(t, res.RecentMessages, 1, "a message the API sends again is read once")
	assert.Equal(t, "msg-1", res.RecentMessages[0]["id"])
}
