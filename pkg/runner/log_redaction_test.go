package runner_test

import (
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
)

// A failed assertion's message reports the actual value it compared, here a
// secret the server echoed back. The engine and container nodes log that
// message, so each log line must be masked like the reported result.

// echoedTokenRequest issues apiToken to /issue, which echoes it back as token,
// and asserts the token is something else, so the node fails.
const echoedTokenRequest = `{"id":"issue","type":"request",
	"data":{"method":"GET","url":"{{baseURL}}/issue?token={{apiToken}}"},
	"assertions":[{"extractor_type":"json_path","extractor_data":{"path":"$.token"},
		"operator_type":"equals","operator_data":{"value":"another-token"}}]}`

func requireMaskedLogLine(t *testing.T, logs, message, secret string) {
	t.Helper()
	if !strings.Contains(logs, `"message":"`+message+`"`) {
		t.Fatalf("expected a %q log line, got:\n%s", message, logs)
	}
	if strings.Contains(logs, secret) {
		t.Errorf("the logs leaked the secret:\n%s", logs)
	}
}

func runWithSecretToken(t *testing.T, definition string) string {
	t.Helper()
	server, _ := chainServer(t)
	flowDef := parseFlow(t, definition, "baseURL", "apiToken")
	logs := captureLogs(t)
	_, _ = runner.Run(flowDef, map[string]any{"baseURL": server.URL, "apiToken": secretValue},
		runner.WithSecretInputKeys([]string{"apiToken"}),
	)
	return logs.String()
}

func TestRun_MasksASecretInputInTheNodeFailureLog(t *testing.T) {
	logs := runWithSecretToken(t, `{"name":"echo","nodes":[`+echoedTokenRequest+`],"edges":[]}`)
	requireMaskedLogLine(t, logs, "Node execution failed", secretValue)
}

func TestRun_MasksASecretOutputInTheNodeFailureLog(t *testing.T) {
	server := newTokenServer(t)
	flowDef := parseFlow(t, `{"name":"mint","nodes":[
		{"id":"mint","type":"request","data":{"method":"POST","url":"{{baseURL}}/token"},
		 "outputs":[{"name":"jwt","secret":true,"extractor":{"type":"json_path","path":"$.jwt"}}],
		 "assertions":[{"extractor_type":"json_path","extractor_data":{"path":"$.jwt"},
			"operator_type":"equals","operator_data":{"value":"another-token"}}]}
	],"edges":[]}`, "baseURL")

	logs := captureLogs(t)
	_, _ = runner.Run(flowDef, map[string]any{"baseURL": server.URL})
	requireMaskedLogLine(t, logs.String(), "Node execution failed", mintedToken)
}

func TestRun_MasksASecretInputInTheLoopIterationFailureLog(t *testing.T) {
	logs := runWithSecretToken(t, `{"name":"loop","nodes":[{"id":"loop","type":"loop","assertions":[],
		"data":{"items":[1],"continue_on_error":true,
		"body":{"name":"body","nodes":[`+echoedTokenRequest+`],"edges":[]}}}],"edges":[]}`)
	requireMaskedLogLine(t, logs, "Loop iteration failed; continuing", secretValue)
}

func TestRun_MasksASecretInputInThePollBodyFailureLog(t *testing.T) {
	logs := runWithSecretToken(t, `{"name":"poll","nodes":[{"id":"poll","type":"poll",
		"assertions":[{"extractor_type":"json_path","extractor_data":{"path":"$.status"},
			"operator_type":"equals","operator_data":{"value":"done"}}],
		"data":{"max_attempts":1,"interval_ms":1,
		"body":{"name":"body","nodes":[`+echoedTokenRequest+`],"edges":[]}}}],"edges":[]}`)
	requireMaskedLogLine(t, logs, "Poll body execution failed", secretValue)
}

func TestRun_MasksASecretInputInThePollExitConditionLog(t *testing.T) {
	logs := runWithSecretToken(t, `{"name":"poll","nodes":[{"id":"poll","type":"poll",
		"assertions":[{"extractor_type":"json_path","extractor_data":{"path":"$['issue.token']"},
			"operator_type":"equals","operator_data":{"value":"another-token"}}],
		"data":{"max_attempts":1,"interval_ms":1,"body":{"name":"body","nodes":[
			{"id":"issue","type":"request","data":{"method":"GET","url":"{{baseURL}}/issue?token={{apiToken}}"},
			 "outputs":[{"name":"token","extractor":{"type":"json_path","path":"$.token"}}]}
		],"edges":[]}}}],"edges":[]}`)
	requireMaskedLogLine(t, logs, "Poll exit condition not yet met", secretValue)
}
