package runner_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
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
	return flow.NewBuilder("secret").
		Input("baseURL", baseURL).
		Input("apiToken", secretValue).
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
