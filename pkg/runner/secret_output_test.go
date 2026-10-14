package runner_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const mintedToken = "minted-session-token-must-never-be-reported"

// tokenServer issues mintedToken from /token and records the Authorization
// header /me receives, so a test can prove downstream nodes still send the
// real value.
type tokenServer struct {
	*httptest.Server

	mu         sync.Mutex
	authHeader string
}

func newTokenServer(t *testing.T) *tokenServer {
	t.Helper()
	server := &tokenServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"jwt":"` + mintedToken + `","session":"sess_visible"}`))
		default:
			server.mu.Lock()
			server.authHeader = r.Header.Get("Authorization")
			server.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *tokenServer) receivedAuth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authHeader
}

// tokenNodes mints the token in "mint" and spends it in "me". secret sets the
// jwt output's secret flag.
func tokenNodes(secret bool) string {
	return `[
		{"id":"mint","type":"request","data":{"method":"POST","url":"{{baseURL}}/token"},
		 "outputs":[
			{"name":"jwt","secret":` + jsonBool(secret) + `,"extractor":{"type":"json_path","path":"$.jwt"}},
			{"name":"session","extractor":{"type":"json_path","path":"$.session"}}
		 ]},
		{"id":"me","type":"request","data":{"method":"GET","url":"{{baseURL}}/me",
		 "headers":{"Authorization":"Bearer {{mint.jwt}}"}}}
	]`
}

func jsonBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestRun_MasksASecretOutputInEveryReportAfterItsExtraction(t *testing.T) {
	server := newTokenServer(t)
	observer := &recordingObserver{}
	flowDef := parseFlow(t, `{"name":"token","nodes":`+tokenNodes(true)+`,
		"edges":[{"id":"e","source":"mint","target":"me","type":"success"}]}`, "baseURL")

	result, err := runner.Run(flowDef, map[string]any{"baseURL": server.URL}, runner.WithObserver(observer))
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
		if strings.Contains(encoded, mintedToken) {
			t.Errorf("%s leaked the secret output: %s", name, encoded)
		}
		if !strings.Contains(encoded, "sess_visible") {
			t.Errorf("%s should keep the output that is not secret: %s", name, encoded)
		}
	}
	if got := server.receivedAuth(); got != "Bearer "+mintedToken {
		t.Errorf("downstream node should send the real token, server received %q", got)
	}
}

// The node that extracts the secret reports first, so its own response body
// must already be masked in its progress event.
func TestRun_MasksASecretOutputInTheResponseThatProducedIt(t *testing.T) {
	server := newTokenServer(t)
	observer := &recordingObserver{}
	flowDef := parseFlow(t, `{"name":"token","nodes":`+tokenNodes(true)+`,
		"edges":[{"id":"e","source":"mint","target":"me","type":"success"}]}`, "baseURL")

	if _, err := runner.Run(flowDef, map[string]any{"baseURL": server.URL}, runner.WithObserver(observer)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(observer.nodeResults) == 0 {
		t.Fatal("expected node.finished events")
	}
	first := encodeJSON(t, observer.nodeResults[0])
	if !strings.Contains(first, `"node_id":"mint"`) {
		t.Fatalf("first event should be the minting node: %s", first)
	}
	if strings.Contains(first, mintedToken) {
		t.Errorf("minting node's own event leaked the token: %s", first)
	}
	if !strings.Contains(first, redact.Mask) {
		t.Errorf("minting node's event should carry the mask: %s", first)
	}
}

func TestRun_ReportsAnOutputThatIsNotSecret(t *testing.T) {
	server := newTokenServer(t)
	flowDef := parseFlow(t, `{"name":"token","nodes":`+tokenNodes(false)+`,
		"edges":[{"id":"e","source":"mint","target":"me","type":"success"}]}`, "baseURL")

	result, err := runner.Run(flowDef, map[string]any{"baseURL": server.URL})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if encoded := encodeJSON(t, result); !strings.Contains(encoded, mintedToken) {
		t.Errorf("an output without the secret flag should be reported as is: %s", encoded)
	}
}

// A module re-exports its child's secret output; the parent's module result,
// child_final_outputs and every consumer of the binding must stay masked.
func TestRun_MasksASecretOutputReExportedByAModule(t *testing.T) {
	server := newTokenServer(t)
	observer := &recordingObserver{}
	parent := parseFlow(t, `{
		"name":"parent",
		"nodes":[
			{"id":"tester","type":"module","data":{"flow_id":"mint-module",
			 "output_bindings":{"token":"mint.jwt"}}},
			{"id":"me","type":"request","data":{"method":"GET","url":"{{baseURL}}/me",
			 "headers":{"Authorization":"Bearer {{tester.token}}"}}}
		],
		"edges":[{"id":"e","source":"tester","target":"me","type":"success"}]
	}`, "baseURL")
	child := json.RawMessage(`{"name":"mint-module","nodes":[
		{"id":"mint","type":"request","data":{"method":"POST","url":"{{baseURL}}/token"},
		 "outputs":[{"name":"jwt","secret":true,"extractor":{"type":"json_path","path":"$.jwt"}}]}
	],"edges":[]}`)

	result, err := runner.Run(parent, map[string]any{"baseURL": server.URL},
		runner.WithObserver(observer),
		runner.WithReferencedFlows(flow.ReferencedFlowRegistry{"mint-module": {FlowDefinition: child}}),
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Success {
		t.Fatalf("flow should succeed, error=%v", result.Error)
	}
	for name, value := range map[string]any{"flow result": result, "node.finished event": observer.nodeResults} {
		if encoded := encodeJSON(t, value); strings.Contains(encoded, mintedToken) {
			t.Errorf("%s leaked the module's secret output: %s", name, encoded)
		}
	}
	if got := server.receivedAuth(); got != "Bearer "+mintedToken {
		t.Errorf("module consumer should send the real token, server received %q", got)
	}
}

// The mint response carries the token, but a sibling output fails to extract,
// so the node fails before reporting jwt. Its response is still reported.
func TestRun_MasksASecretOutputWhenAnotherOutputOfItsNodeFails(t *testing.T) {
	server := newTokenServer(t)
	observer := &recordingObserver{}
	flowDef := parseFlow(t, `{"name":"token","nodes":[
		{"id":"mint","type":"request","data":{"method":"POST","url":"{{baseURL}}/token"},
		 "outputs":[
			{"name":"jwt","secret":true,"extractor":{"type":"json_path","path":"$.jwt"}},
			{"name":"absent","extractor":{"type":"json_path","path":"$.absent"}}
		 ]}
	],"edges":[]}`, "baseURL")

	result, err := runner.Run(flowDef, map[string]any{"baseURL": server.URL}, runner.WithObserver(observer))
	if err == nil || result == nil || result.Success {
		t.Fatalf("the flow should report a failure on the absent output, err=%v", err)
	}
	for name, value := range map[string]any{"flow result": result, "node.finished event": observer.nodeResults} {
		if encoded := encodeJSON(t, value); strings.Contains(encoded, mintedToken) {
			t.Errorf("%s leaked the secret output of a failed node: %s", name, encoded)
		}
	}
}

const shortPin = "4821"

// pinServer issues a body holding a long token, a short pin and a short state
// from /token, and answers every other path with the same short state.
func pinServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"jwt":"` + mintedToken + `","pin":"` + shortPin + `","state":"ok"}`))
			return
		}
		_, _ = w.Write([]byte(`{"state":"ok","pin_seen":"` + r.Header.Get("X-Pin") + `"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func nodeResult(t *testing.T, result *spi.FlowExecutionResult, nodeID string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(encodeJSON(t, result.ExecutionResults[nodeID])), &decoded); err != nil {
		t.Fatalf("decode %s: %v", nodeID, err)
	}
	return decoded
}

// A pin is too short to mask by value without masking every "4821" in the run,
// so it is masked by name, and the response that produced it is dropped.
func TestRun_MasksASecretOutputTooShortToMaskByValueByName(t *testing.T) {
	server := pinServer(t)
	flowDef := parseFlow(t, `{"name":"pin","nodes":[
		{"id":"mint","type":"request","data":{"method":"POST","url":"{{baseURL}}/token"},
		 "outputs":[{"name":"pin","secret":true,"extractor":{"type":"json_path","path":"$.pin"}}]},
		{"id":"me","type":"request","data":{"method":"GET","url":"{{baseURL}}/me",
		 "headers":{"X-Pin":"{{mint.pin}}"}}}
	],"edges":[{"id":"e","source":"mint","target":"me","type":"success"}]}`, "baseURL")

	result, err := runner.Run(flowDef, map[string]any{"baseURL": server.URL})
	if err != nil || !result.Success {
		t.Fatalf("flow should succeed, err=%v", err)
	}

	if got := result.FinalOutputs["mint.pin"]; got != redact.Mask {
		t.Errorf("final_outputs should mask the pin by name, got %v", got)
	}
	mint := nodeResult(t, result, "mint")
	if outputs, _ := mint["outputs"].(map[string]any); outputs["pin"] != redact.Mask {
		t.Errorf("the minting node should mask the pin by name: %v", mint)
	}
	if _, present := mint["response_body"]; present {
		t.Errorf("the response that produced the pin should be dropped: %v", mint)
	}
	if inputs, _ := nodeResult(t, result, "me")["inputs"].(map[string]any); inputs["mint.pin"] != redact.Mask {
		t.Errorf("the consumer's input should mask the pin by name: %v", inputs)
	}
}

// A body flagged secret has leaves like "ok" that other nodes report too. Only
// its long leaves are masked by value, so those reports stay readable.
func TestRun_KeepsShortLeavesOfASecretBodyReadableInOtherNodes(t *testing.T) {
	server := pinServer(t)
	flowDef := parseFlow(t, `{"name":"body","nodes":[
		{"id":"mint","type":"request","data":{"method":"POST","url":"{{baseURL}}/token"},
		 "outputs":[{"name":"payload","secret":true,"extractor":{"type":"body"}}]},
		{"id":"me","type":"request","data":{"method":"GET","url":"{{baseURL}}/me",
		 "headers":{"Authorization":"Bearer {{mint.payload.jwt}}"}},
		 "outputs":[{"name":"state","extractor":{"type":"json_path","path":"$.state"}}]}
	],"edges":[{"id":"e","source":"mint","target":"me","type":"success"}]}`, "baseURL")

	result, err := runner.Run(flowDef, map[string]any{"baseURL": server.URL})
	if err != nil || !result.Success {
		t.Fatalf("flow should succeed, err=%v", err)
	}

	if encoded := encodeJSON(t, result); strings.Contains(encoded, mintedToken) {
		t.Errorf("the long leaf of the secret body leaked: %s", encoded)
	}
	if got := result.FinalOutputs["me.state"]; got != "ok" {
		t.Errorf("another node's short value should stay readable, got %v", got)
	}
}

// A module output bound to a child output masked by name carries the same
// short value, so it is masked by name too.
func TestRun_MasksAModuleOutputBoundToAShortSecretOutputByName(t *testing.T) {
	server := pinServer(t)
	parent := parseFlow(t, `{"name":"parent","nodes":[
		{"id":"tester","type":"module","data":{"flow_id":"pin-module","output_bindings":{"code":"mint.pin"}}}
	],"edges":[]}`, "baseURL")
	child := json.RawMessage(`{"name":"pin-module","nodes":[
		{"id":"mint","type":"request","data":{"method":"POST","url":"{{baseURL}}/token"},
		 "outputs":[{"name":"pin","secret":true,"extractor":{"type":"json_path","path":"$.pin"}}]}
	],"edges":[]}`)

	result, err := runner.Run(parent, map[string]any{"baseURL": server.URL},
		runner.WithReferencedFlows(flow.ReferencedFlowRegistry{"pin-module": {FlowDefinition: child}}),
	)
	if err != nil || !result.Success {
		t.Fatalf("flow should succeed, err=%v", err)
	}

	if got := result.FinalOutputs["tester.code"]; got != redact.Mask {
		t.Errorf("the module output should be masked by name, got %v", got)
	}
	tester := nodeResult(t, result, "tester")
	if children, _ := tester["child_final_outputs"].(map[string]any); children["mint.pin"] != redact.Mask {
		t.Errorf("child_final_outputs should mask the pin by name: %v", tester)
	}
}

// A secret output extracted as a JSON number is reported as a number, which
// has no string to scan, in its node's outputs, final_outputs, and the inputs
// and typed request body of the node that spends it.
func TestRun_MasksANumericSecretOutputInEveryReport(t *testing.T) {
	var received atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"num":123456}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		received.Store(string(body))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	flowDef := parseFlow(t, `{"name":"num","nodes":[
		{"id":"mint","type":"request","data":{"method":"POST","url":"{{baseURL}}/token"},
		 "outputs":[{"name":"num","secret":true,"extractor":{"type":"json_path","path":"$.num"}}]},
		{"id":"read","type":"request","data":{"method":"POST","url":"{{baseURL}}/read",
		 "body":{"typed":"{{{mint.num}}}"}}}
	],"edges":[{"id":"e","source":"mint","target":"read","type":"success"}]}`, "baseURL")
	observer := &recordingObserver{}

	result, err := runner.Run(flowDef, map[string]any{"baseURL": server.URL}, runner.WithObserver(observer))
	if err != nil || !result.Success || len(observer.nodeResults) == 0 {
		t.Fatalf("flow should succeed with node.finished events, err=%v", err)
	}

	if outputs, _ := nodeResult(t, result, "mint")["outputs"].(map[string]any); outputs["num"] != redact.Mask {
		t.Errorf("the minting node should mask the number: %v", outputs)
	}
	if got := result.FinalOutputs["mint.num"]; got != redact.Mask {
		t.Errorf("final_outputs should mask the number, got %v", got)
	}
	read := nodeResult(t, result, "read")
	if inputs, _ := read["inputs"].(map[string]any); inputs["mint.num"] != redact.Mask {
		t.Errorf("the consumer's input should mask the number: %v", inputs)
	}
	if body, _ := read["request_body"].(map[string]any); body["typed"] != redact.Mask {
		t.Errorf("the consumer's typed request body should mask the number: %v", read["request_body"])
	}
	if parsed, _ := nodeResult(t, result, "mint")["response_body_parsed"].(map[string]any); parsed["num"] != redact.Mask {
		t.Errorf("the response that produced the number should mask it: %v", parsed)
	}
	if first := encodeJSON(t, observer.nodeResults[0]); !strings.Contains(first, `"num":"`+redact.Mask+`"`) ||
		strings.Contains(first, `"num":123456`) {
		t.Errorf("the minting node's node.finished event should mask the number: %s", first)
	}
	if got, _ := received.Load().(string); got != `{"typed":123456}` {
		t.Errorf("the consumer should send the real number, server received %q", got)
	}
}
