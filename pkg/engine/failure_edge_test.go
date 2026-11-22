package engine_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingPathServer answers 200 except on the given paths, which answer 500.
func failingPathServer(t *testing.T, failing ...string) (*httptest.Server, *recordedCalls) {
	t.Helper()
	calls := &recordedCalls{}
	fail := make(map[string]bool, len(failing))
	for _, path := range failing {
		fail[path] = true
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.add(r.Method + " " + r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if fail[r.URL.Path] {
			w.WriteHeader(http.StatusInternalServerError)
		}
		_, _ = w.Write([]byte(`{"id":"org_1"}`))
	}))
	t.Cleanup(server.Close)
	return server, calls
}

// requestNode is a request node that fails unless the response is 200.
func requestNode(id, runWhen, method, url string) string {
	runWhenField := ""
	if runWhen != "" {
		runWhenField = `"run_when": "` + runWhen + `",`
	}
	return `{
		"id": "` + id + `",
		"display_name": "` + id + `",
		"type": "request",
		` + runWhenField + `
		"data": {"method": "` + method + `", "url": "` + url + `", "timeout": 1000},
		"assertions": [{
			"extractor_type": "status_code",
			"extractor_data": {},
			"operator_type": "equals",
			"operator_data": {"value": 200}
		}]
	}`
}

func edgeJSON(source, target, edgeType string) string {
	return `{"id": "` + source + "-" + target + "-" + edgeType + `", "source": "` + source +
		`", "target": "` + target + `", "type": "` + edgeType + `"}`
}

func runFailureEdgeFlow(t *testing.T, nodes, edges []string) *spi.FlowExecutionResult {
	t.Helper()
	definition := `{"name": "on_failure", "version": "1.0", "nodes": [` + strings.Join(nodes, ",") +
		`], "edges": [` + strings.Join(edges, ",") + `]}`
	parsed, err := flow.ParseFromJSON([]byte(definition))
	require.NoError(t, err)
	result, _ := engine.ExecuteFlowDefinition(*parsed, nil, nil)
	require.NotNil(t, result)
	return result
}

// selfCleaningOrgFlow mirrors the function flow that leaked an org: each setup
// step undoes its own partial work through on_failure.
//
//	create-org --on_failure--> drop-key-no-org
//	    |
//	workspace --on_failure--> undo-org --> drop-key-after-undo
func selfCleaningOrgFlow(serverURL string) ([]string, []string) {
	nodes := []string{
		requestNode("create-org", "", "POST", serverURL+"/orgs"),
		requestNode("workspace", "", "POST", serverURL+"/workspaces"),
		requestNode("drop-key-no-org", "always", "DELETE", serverURL+"/keys/no-org"),
		requestNode("undo-org", "always", "DELETE", serverURL+"/orgs/org_1"),
		requestNode("drop-key-after-undo", "always", "DELETE", serverURL+"/keys/after-undo"),
	}
	edges := []string{
		edgeJSON("create-org", "workspace", "success"),
		edgeJSON("create-org", "drop-key-no-org", "failure"),
		edgeJSON("workspace", "undo-org", "failure"),
		edgeJSON("undo-org", "drop-key-after-undo", "success"),
	}
	return nodes, edges
}

func skipReasonOf(t *testing.T, result spi.AnyResult) (string, string) {
	t.Helper()
	requireSkipped(t, result)
	base, _ := skippedBaseOf(result)
	message := ""
	if base.ErrorMsg != nil {
		message = *base.ErrorMsg
	}
	return *base.SkipReason, message
}

func TestFailureEdge_SourceSucceeds_SkipsTheFailurePath(t *testing.T) {
	server, calls := failingPathServer(t)
	nodes, edges := selfCleaningOrgFlow(server.URL)

	result := runFailureEdgeFlow(t, nodes, edges)

	require.True(t, result.Success, "a skipped failure path does not fail the run: %v", result.Error)
	assert.Equal(t, []string{"POST /orgs", "POST /workspaces"}, calls.all())
	for _, id := range []string{"drop-key-no-org", "undo-org"} {
		reason, _ := skipReasonOf(t, result.ExecutionResults[id])
		assert.Equal(t, "routed_away_by_branch", reason, id)
	}
	// Like a node below a routed-away branch arm, the chain behind the failure
	// target is skipped without a routing reason of its own.
	requireSkipped(t, result.ExecutionResults["drop-key-after-undo"])
	_, message := skipReasonOf(t, result.ExecutionResults["drop-key-no-org"])
	assert.Equal(t, `Skipped because step "create-org" did not fail; this step runs only on its failure`, message)
}

func TestFailureEdge_SourceFails_RunsItsTargetOnly(t *testing.T) {
	server, calls := failingPathServer(t, "/orgs")
	nodes, edges := selfCleaningOrgFlow(server.URL)

	result := runFailureEdgeFlow(t, nodes, edges)

	require.False(t, result.Success)
	assert.Equal(t, []string{"POST /orgs", "DELETE /keys/no-org"}, calls.all(),
		"workspace was skipped, not failed, so its on_failure path stays dead")
	reason, _ := skipReasonOf(t, result.ExecutionResults["workspace"])
	assert.Equal(t, "dependency_failed", reason)
	reason, _ = skipReasonOf(t, result.ExecutionResults["undo-org"])
	assert.Equal(t, "routed_away_by_branch", reason)
	requireSkipped(t, result.ExecutionResults["drop-key-after-undo"])
}

func TestFailureEdge_LaterSourceFails_RunsItsFailureChain(t *testing.T) {
	server, calls := failingPathServer(t, "/workspaces")
	nodes, edges := selfCleaningOrgFlow(server.URL)

	result := runFailureEdgeFlow(t, nodes, edges)

	require.False(t, result.Success)
	assert.Equal(t,
		[]string{"POST /orgs", "POST /workspaces", "DELETE /orgs/org_1", "DELETE /keys/after-undo"},
		calls.all())
	reason, _ := skipReasonOf(t, result.ExecutionResults["drop-key-no-org"])
	assert.Equal(t, "routed_away_by_branch", reason)
}

func TestFailureEdge_MixedPredecessors(t *testing.T) {
	tests := []struct {
		name    string
		failing []string
		edgeB   string
		wantRun bool
	}{
		{name: "dead failure edge and live success edge", edgeB: "success", wantRun: true},
		{name: "two failure edges, neither source fails", edgeB: "failure", wantRun: false},
		{name: "two failure edges, one source fails", failing: []string{"/b"}, edgeB: "failure", wantRun: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, calls := failingPathServer(t, tt.failing...)
			nodes := []string{
				requestNode("a", "", "POST", server.URL+"/a"),
				requestNode("b", "", "POST", server.URL+"/b"),
				requestNode("cleanup", "always", "DELETE", server.URL+"/cleanup"),
			}
			edges := []string{
				edgeJSON("a", "cleanup", "failure"),
				edgeJSON("b", "cleanup", tt.edgeB),
			}

			result := runFailureEdgeFlow(t, nodes, edges)

			if tt.wantRun {
				assert.Contains(t, calls.all(), "DELETE /cleanup")
				return
			}
			assert.NotContains(t, calls.all(), "DELETE /cleanup")
			reason, _ := skipReasonOf(t, result.ExecutionResults["cleanup"])
			assert.Equal(t, "routed_away_by_branch", reason)
		})
	}
}

func TestFailureEdge_SameSourceWithSuccessAndFailureEdge_StaysLive(t *testing.T) {
	server, calls := failingPathServer(t)
	nodes := []string{
		requestNode("a", "", "POST", server.URL+"/a"),
		requestNode("cleanup", "always", "DELETE", server.URL+"/cleanup"),
	}
	edges := []string{
		edgeJSON("a", "cleanup", "success"),
		edgeJSON("a", "cleanup", "failure"),
	}

	result := runFailureEdgeFlow(t, nodes, edges)

	require.True(t, result.Success, "%v", result.Error)
	assert.Equal(t, []string{"POST /a", "DELETE /cleanup"}, calls.all())
}

func TestFailureEdge_FromAnAlwaysNode(t *testing.T) {
	for _, tt := range []struct {
		name     string
		failing  []string
		wantCall bool
	}{
		{name: "cleanup succeeds", wantCall: false},
		{name: "cleanup fails", failing: []string{"/cleanup"}, wantCall: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, calls := failingPathServer(t, tt.failing...)
			nodes := []string{
				requestNode("a", "", "POST", server.URL+"/a"),
				requestNode("cleanup", "always", "DELETE", server.URL+"/cleanup"),
				requestNode("alert", "always", "POST", server.URL+"/alert"),
			}
			edges := []string{
				edgeJSON("a", "cleanup", "success"),
				edgeJSON("cleanup", "alert", "failure"),
			}

			result := runFailureEdgeFlow(t, nodes, edges)

			if tt.wantCall {
				assert.Equal(t, []string{"POST /a", "DELETE /cleanup", "POST /alert"}, calls.all())
				return
			}
			assert.Equal(t, []string{"POST /a", "DELETE /cleanup"}, calls.all())
			reason, _ := skipReasonOf(t, result.ExecutionResults["alert"])
			assert.Equal(t, "routed_away_by_branch", reason)
		})
	}
}

// An on_success step placed after a cleanup runs in the always phase; its
// failure edge is judged there like any other.
func TestFailureEdge_FromAStepAfterACleanup(t *testing.T) {
	server, calls := failingPathServer(t, "/a")
	nodes := []string{
		requestNode("a", "", "POST", server.URL+"/a"),
		requestNode("cleanup", "always", "DELETE", server.URL+"/cleanup"),
		requestNode("verify", "", "GET", server.URL+"/verify"),
		requestNode("undo-verify", "always", "DELETE", server.URL+"/verify"),
	}
	edges := []string{
		edgeJSON("a", "cleanup", "success"),
		edgeJSON("cleanup", "verify", "success"),
		edgeJSON("verify", "undo-verify", "failure"),
	}

	result := runFailureEdgeFlow(t, nodes, edges)

	require.False(t, result.Success)
	assert.Equal(t, []string{"POST /a", "DELETE /cleanup", "GET /verify"}, calls.all())
	reason, _ := skipReasonOf(t, result.ExecutionResults["undo-verify"])
	assert.Equal(t, "routed_away_by_branch", reason)
}
