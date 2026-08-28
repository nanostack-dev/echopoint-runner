package runner_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const secretValue = "sk-live-must-never-be-reported"

// recordingObserver keeps the results carried by the progress events, which are
// what a transport ships to the control plane.
type recordingObserver struct {
	engine.NoopObserver

	nodeResults []spi.AnyResult
	flowResult  *spi.FlowExecutionResult
}

func (o *recordingObserver) NodeFinished(evt engine.NodeFinishedEvent) {
	o.nodeResults = append(o.nodeResults, evt.Result)
}

func (o *recordingObserver) FlowFinished(evt engine.FlowFinishedEvent) {
	o.flowResult = evt.Result
}

// secretFlow sends the secret input through every request field a result echoes
// back: the URL query, a header, and the body. The server reflects it into a
// response header too.
func secretFlow(baseURL string) flow.Flow {
	return secretFlowWith(baseURL, secretValue)
}

func secretFlowWith(baseURL, secret string) flow.Flow {
	return flow.NewBuilder("secret").
		Input("baseURL", baseURL).
		Input("apiToken", secret).
		Add(node.NewRequest("call").
			POST("{{baseURL}}/resource?token={{apiToken}}").
			Header("Authorization", "Bearer {{apiToken}}").
			Body(map[string]any{"token": "{{apiToken}}"})).
		Build()
}

func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echoed-Token", r.URL.Query().Get("token"))
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func encodeJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}

func TestRun_MasksSecretInputValuesInResultsAndEvents(t *testing.T) {
	server := echoServer(t)
	observer := &recordingObserver{}

	result, err := runner.Run(secretFlow(server.URL), nil,
		runner.WithObserver(observer),
		runner.WithSecretInputKeys([]string{"apiToken"}),
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Success {
		t.Fatalf("flow should succeed, error=%v", result.Error)
	}

	reported := map[string]any{
		"flow result":         result,
		"flow.finished event": observer.flowResult,
		"node.finished event": observer.nodeResults,
	}
	for name, value := range reported {
		encoded := encodeJSON(t, value)
		if strings.Contains(encoded, secretValue) {
			t.Errorf("%s leaked the secret value: %s", name, encoded)
		}
		if !strings.Contains(encoded, redact.Mask) {
			t.Errorf("%s should carry the mask in the secret's place: %s", name, encoded)
		}
	}
}

// The masked node result must still report the node identity, timing and
// outcome the control plane keys progress events on.
func TestRun_MaskedNodeResultKeepsNonSecretFields(t *testing.T) {
	server := echoServer(t)
	observer := &recordingObserver{}

	if _, err := runner.Run(secretFlow(server.URL), nil,
		runner.WithObserver(observer),
		runner.WithSecretInputKeys([]string{"apiToken"}),
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(observer.nodeResults) != 1 {
		t.Fatalf("expected 1 node.finished event, got %d", len(observer.nodeResults))
	}
	nodeResult := observer.nodeResults[0]
	if nodeResult.GetNodeID() != "call" {
		t.Errorf("node id lost: %q", nodeResult.GetNodeID())
	}
	if nodeResult.GetNodeType() != spi.KindRequest {
		t.Errorf("node type lost: %q", nodeResult.GetNodeType())
	}
	if nodeResult.GetError() != nil {
		t.Errorf("unexpected error: %v", nodeResult.GetError())
	}

	encoded := encodeJSON(t, nodeResult)
	if !strings.Contains(encoded, `"response_status_code":200`) {
		t.Errorf("response status lost from masked result: %s", encoded)
	}
}

// Without secret keys nothing is masked — the runner reports the values a flow
// author expects to see.
func TestRun_ReportsValuesWhenNoInputIsSecret(t *testing.T) {
	server := echoServer(t)

	result, err := runner.Run(secretFlow(server.URL), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if encoded := encodeJSON(t, result); !strings.Contains(encoded, secretValue) {
		t.Errorf("value should be reported when it is not declared secret: %s", encoded)
	}
}

// escapedSecret holds every character json.Marshal escapes, so a scan of the
// encoded bytes cannot find it.
const escapedSecret = `p@ss&w"rd<x>\y`

// (a) A server that echoes the token into its response body. The raw body is
// reported as base64 (RequestExecutionResult.ResponseBody is []byte), where a
// plaintext scan is blind.
func TestRun_MasksTheSecretEchoedInTheResponseBody(t *testing.T) {
	assertEchoedSecretIsMasked(t, secretValue)
}

// (a2) Both blind spots at once: a server echoing an escapable secret into its
// own JSON response. The reported response_body decodes to that JSON, where the
// server's encoder has escaped the secret — so it has no literal form there
// either.
func TestRun_MasksAnEscapableSecretEchoedInTheResponseBody(t *testing.T) {
	assertEchoedSecretIsMasked(t, escapedSecret)
}

func assertEchoedSecretIsMasked(t *testing.T, secret string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The header, not the query: a secret holding & is cut in two by the query
		// parser, so only the header carries it back whole.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"echoed":        r.URL.Query().Get("token"),
			"authorization": r.Header.Get("Authorization"),
		})
	}))
	t.Cleanup(server.Close)

	observer := &recordingObserver{}
	if _, err := runner.Run(secretFlowWith(server.URL, secret), nil,
		runner.WithObserver(observer),
		runner.WithSecretInputKeys([]string{"apiToken"}),
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	if leaks(t, observer.nodeResults[0], secret) {
		t.Errorf("node result leaked the echoed secret: %s", encodeJSON(t, observer.nodeResults[0]))
	}
}

// (b) A secret holding the characters json.Marshal escapes.
func TestRun_MasksASecretThatJSONEscapes(t *testing.T) {
	server := echoServer(t)
	observer := &recordingObserver{}

	result, err := runner.Run(secretFlowWith(server.URL, escapedSecret), nil,
		runner.WithObserver(observer),
		runner.WithSecretInputKeys([]string{"apiToken"}),
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for name, value := range map[string]any{
		"flow result":         result,
		"node.finished event": observer.nodeResults,
	} {
		if leaks(t, value, escapedSecret) {
			t.Errorf("%s leaked the escaped secret: %s", name, encodeJSON(t, value))
		}
	}
}

// (c) Logs are never redacted — the redactor runs on results, not on log lines —
// so no producer may echo a resolved value at any level.
func TestRun_NoLogLineCarriesTheSecret(t *testing.T) {
	logs := captureLogs(t)

	// A successful chain (the value flows through inputs, an extracted output and
	// a downstream header) and a transport failure (the error chain carries the
	// resolved URL) between them touch every producer that echoed a value.
	server, _ := chainServer(t)
	if _, err := runner.Run(chainedSecretFlow(t, server.URL), nil,
		runner.WithSecretInputKeys([]string{"apiToken"}),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := runner.Run(secretFlow("http://127.0.0.1:1"), nil,
		runner.WithSecretInputKeys([]string{"apiToken"}),
	); err == nil {
		t.Fatal("the failing flow should error")
	}
	// The two container nodes report a child-flow failure themselves, outside
	// the engine's node-failure line, and the child error chain carries the
	// resolved URL.
	if _, err := runner.Run(unreachablePollFlow(t), nil,
		runner.WithSecretInputKeys([]string{"apiToken"}),
	); err == nil {
		t.Fatal("the failing poll flow should error")
	}
	if _, err := runner.Run(unreachableLoopFlow(t), nil,
		runner.WithSecretInputKeys([]string{"apiToken"}),
	); err != nil {
		t.Fatalf("continue_on_error should absorb the iteration failure: %v", err)
	}

	for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
		if strings.Contains(line, secretValue) {
			t.Errorf("log line carries the secret: %s", line)
		}
	}
}

// (d) A transport failure against a URL carrying the secret. The wire
// error_message is what echopoint stores and shows.
func TestRun_MasksTheSecretInTheReportedErrorMessage(t *testing.T) {
	// Port 1 is never listening, so the request fails during connect.
	result, err := runner.Run(secretFlowWith("http://127.0.0.1:1", escapedSecret), nil,
		runner.WithSecretInputKeys([]string{"apiToken"}),
	)
	if err == nil {
		t.Fatal("the flow should fail")
	}
	if strings.Contains(err.Error(), escapedSecret) {
		t.Errorf("returned error leaked the secret: %v", err)
	}
	if result.ErrorMsg != nil && strings.Contains(*result.ErrorMsg, escapedSecret) {
		t.Errorf("wire error_message leaked the secret: %s", *result.ErrorMsg)
	}
	if leaks(t, result, escapedSecret) {
		t.Errorf("failed flow result leaked the secret: %s", encodeJSON(t, result))
	}
}

// (e) Masking is applied to a copy at the reporting boundary: a downstream node
// consuming a secret-derived output still receives the real value.
func TestRun_MasksACopySoDownstreamNodesKeepTheRealValue(t *testing.T) {
	server, authorization := chainServer(t)

	observer := &recordingObserver{}
	result, err := runner.Run(chainedSecretFlow(t, server.URL), nil,
		runner.WithObserver(observer),
		runner.WithSecretInputKeys([]string{"apiToken"}),
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Success {
		t.Fatalf("flow should succeed, error=%v", result.Error)
	}

	if got := authorization(); got != "Bearer "+secretValue {
		t.Errorf("the second node must receive the real value, got %q", got)
	}
	for name, value := range map[string]any{
		"flow result":          result,
		"flow.finished event":  observer.flowResult,
		"node.finished events": observer.nodeResults,
	} {
		if leaks(t, value, secretValue) {
			t.Errorf("%s leaked the secret-derived output: %s", name, encodeJSON(t, value))
		}
	}
}

// unreachableBody is an inline body flow whose only request targets a port
// nothing listens on, with the secret in the query string — so the child flow
// always fails and its error chain carries the resolved URL.
const unreachableBody = `{"name":"body","nodes":[{"id":"probe","type":"request",` +
	`"data":{"method":"GET","url":"http://127.0.0.1:1/status?token={{apiToken}}"}}],"edges":[]}`

func unreachablePollFlow(t *testing.T) flow.Flow {
	t.Helper()
	return secretContainerFlow(t, "poll", `{"id":"poll","type":"poll","assertions":[`+
		`{"extractor_type":"jsonPath","extractor_data":{"path":"$.status"},`+
		`"operator_type":"equals","operator_data":{"value":"done"}}],`+
		`"data":{"body":`+unreachableBody+`,"max_attempts":1,"interval_ms":1}}`)
}

func unreachableLoopFlow(t *testing.T) flow.Flow {
	t.Helper()
	return secretContainerFlow(t, "loop", `{"id":"loop","type":"loop","assertions":[],`+
		`"data":{"items":[1],"body":`+unreachableBody+`,"continue_on_error":true}}`)
}

func secretContainerFlow(t *testing.T, name, rawNode string) flow.Flow {
	t.Helper()
	containerNode, err := node.UnmarshalNode([]byte(rawNode))
	if err != nil {
		t.Fatalf("unmarshal %s node: %v", name, err)
	}
	return flow.NewBuilder(name).
		Input("apiToken", secretValue).
		Add(containerNode).
		Build()
}

// chainServer issues the token back on /issue and records the Authorization
// header it is presented with on /use.
func chainServer(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/issue" {
			_ = json.NewEncoder(w).Encode(map[string]any{"token": r.URL.Query().Get("token")})
			return
		}
		mu.Lock()
		authorization = r.Header.Get("Authorization")
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(server.Close)
	return server, func() string {
		mu.Lock()
		defer mu.Unlock()
		return authorization
	}
}

// chainedSecretFlow extracts the secret out of the first node's response and
// feeds it to the second node's Authorization header.
func chainedSecretFlow(t *testing.T, baseURL string) flow.Flow {
	t.Helper()
	return flow.NewBuilder("chained").
		Input("baseURL", baseURL).
		Input("apiToken", secretValue).
		Add(node.NewRequest("issue").
			GET("{{baseURL}}/issue?token={{apiToken}}").
			Output(jsonPathOutput(t, "token", "$.token"))).
		Add(node.NewRequest("use").
			GET("{{baseURL}}/use").
			Header("Authorization", "Bearer {{issue.token}}")).
		Edge("issue", "use").
		Build()
}

// leaks reports whether secret survives anywhere in the JSON value reports on
// the wire. The search runs on the decoded tree, and decodes base64 leaves:
// scanning the encoded text would miss a secret json.Marshal escaped, and a
// secret a server echoed into the base64 response_body, which is exactly how
// the first redactor missed both.
//
// Every escaped form is searched for too. A decoded response_body is text the
// target server produced: a secret it echoed into its own JSON response sits
// there escaped by its encoder, so a scan for the plain form alone is blind —
// the same blindness the redactor had.
func leaks(t *testing.T, value any, secret string) bool {
	t.Helper()
	var tree any
	if err := json.Unmarshal([]byte(encodeJSON(t, value)), &tree); err != nil {
		t.Fatalf("a reported value must stay valid JSON: %v", err)
	}
	for _, form := range append([]string{secret}, jsonForms(t, secret)...) {
		if treeLeaks(tree, form) {
			return true
		}
	}
	return false
}

// jsonForms returns how secret is written inside JSON text: json.Marshal
// escapes &, < and > as \u00XX, an encoder with SetEscapeHTML(false) leaves
// them alone, and both escape " and \.
func jsonForms(t *testing.T, secret string) []string {
	t.Helper()
	forms := make([]string, 0, 2)
	for _, escapeHTML := range []bool{true, false} {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(escapeHTML)
		if err := encoder.Encode(secret); err != nil {
			t.Fatalf("encode secret: %v", err)
		}
		quoted := strings.TrimRight(buffer.String(), "\n")
		forms = append(forms, quoted[1:len(quoted)-1])
	}
	return forms
}

func treeLeaks(v any, secret string) bool {
	switch value := v.(type) {
	case string:
		if strings.Contains(value, secret) {
			return true
		}
		decoded, err := base64.StdEncoding.DecodeString(value)
		return err == nil && strings.Contains(string(decoded), secret)
	case []any:
		for _, item := range value {
			if treeLeaks(item, secret) {
				return true
			}
		}
	case map[string]any:
		for key, item := range value {
			if strings.Contains(key, secret) || treeLeaks(item, secret) {
				return true
			}
		}
	}
	return false
}

func jsonPathOutput(t *testing.T, name, path string) node.Output {
	t.Helper()
	extractor, err := extractors.UnmarshalExtractor([]byte(`{"type":"jsonPath","path":"` + path + `"}`))
	if err != nil {
		t.Fatalf("extractor: %v", err)
	}
	return node.Output{Name: name, Extractor: extractor}
}

// captureLogs redirects the global logger into a buffer at trace level, so a
// leak at any level is visible, and restores it when the test ends.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	//nolint:reassign // capturing the global logger is the point of this helper
	log.Logger = zerolog.New(zerolog.SyncWriter(buffer))
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		//nolint:reassign // restores what the helper replaced
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})
	return buffer
}
