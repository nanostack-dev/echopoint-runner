package runner_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type runIDVars struct{}

func (runIDVars) Resolve(name string, _ []string) (string, error) {
	if name == "runId" {
		return "run-7", nil
	}
	return "", errors.New("unknown dynamic variable " + name)
}

// An sse node's assertion expected values resolve upstream outputs and dynamic
// variables like every other node's, instead of comparing against the
// template text.
func TestRun_SseAssertionComparesAgainstResolvedTemplates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"ns\":\"ns-42\",\"run\":\"run-7\"}\n\n")
	}))
	t.Cleanup(server.Close)
	flowDef := parseFlow(t, `{"version":"1.0","name":"t","nodes":[
		{"id":"want","type":"set_variable","data":{"variables":{"ns":"ns-42"}}},
		{"id":"stream","type":"sse","data":{"url":"`+server.URL+`/events","timeout_ms":2000},
		 "assertions":[
			{"extractor_type":"json_path","extractor_data":{"path":"$.ns"},
			 "operator_type":"equals","operator_data":{"value":"{{want.ns}}"}},
			{"extractor_type":"json_path","extractor_data":{"path":"$.run"},
			 "operator_type":"equals","operator_data":{"value":"{{$runId}}"}}
		 ]}
	],"edges":[{"id":"e","source":"want","target":"stream","type":"success"}]}`)

	result, err := runner.Run(flowDef, map[string]any{}, runner.WithDynamicVars(runIDVars{}))

	require.NoError(t, err)
	require.True(t, result.Success)
	stream := result.ExecutionResults["stream"]
	assert.Equal(t, "ns-42", stream.GetInputs()["want.ns"], "the referenced output is an input of the sse node")
	results := spi.MustAs[*node.SseExecutionResult](stream).AssertionResults
	require.Len(t, results, 2)
	assert.Equal(t, "ns-42", results[0].Expected)
	assert.Equal(t, "run-7", results[1].Expected)
}
