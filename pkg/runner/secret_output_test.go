package runner_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
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
