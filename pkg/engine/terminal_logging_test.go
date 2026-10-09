package engine_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func captureEngineLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := new(bytes.Buffer)
	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	//nolint:reassign // the engine uses this global logger; serial tests capture its observable severity
	log.Logger = zerolog.New(zerolog.SyncWriter(buffer))
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() {
		//nolint:reassign // restore the logger captured above
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})
	return buffer
}

func TestEngineOwnsExtractionFailureSeverity(t *testing.T) {
	for _, assertionFailure := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "output", true: "assertion-and-best-effort-output"}[assertionFailure],
			func(t *testing.T) {
				logs := captureEngineLogs(t)
				n := &assertionNode{
					BaseNode: node.BaseNode{
						ID: "step", DisplayName: "Step", NodeType: spi.KindRequest,
						Outputs: []node.Output{mkOutput(t, "missing", "$.missing")},
					},
					value: map[string]any{"status": "ok"},
				}
				if assertionFailure {
					n.Assertions = []node.CompositeAssertion{
						mkAssertionJSON(t, "json_path", "$.absent", "equals", "ok"),
						mkAssertionJSON(t, "header", "", "contains", "present"),
					}
				}
				flowEngine, err := engine.NewFlowEngine(
					flow.Flow{Name: "logging", Nodes: []node.AnyNode{n}},
					&engine.Options{},
				)
				require.NoError(t, err)
				_, err = flowEngine.Execute(map[string]any{})
				require.Error(t, err)
				require.NotContains(t, logs.String(), `"level":"error"`)
				require.NotContains(t, logs.String(), `"level":"warn"`)
				require.Equal(t, 1, strings.Count(logs.String(), `"message":"Node execution failed"`))
				require.Contains(t, logs.String(), `"flowName":"logging"`)
				require.Contains(t, logs.String(), `"nodeID":"step"`)
				if assertionFailure {
					require.Contains(t, logs.String(), `"message":"Output unavailable after assertion failure"`)
					require.Contains(t, logs.String(), `"outputName":"missing"`)
				}
			},
		)
	}
}

func TestEngineOwnsSSETerminalFailureSeverity(t *testing.T) {
	logs := captureEngineLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	n := &node.SseNode{
		BaseNode: node.BaseNode{ID: "stream", DisplayName: "Stream", NodeType: spi.KindSse},
		Data:     node.SseData{URL: server.URL, TimeoutMs: 2000},
	}
	flowEngine, err := engine.NewFlowEngine(flow.Flow{Name: "logging", Nodes: []node.AnyNode{n}}, &engine.Options{})
	require.NoError(t, err)
	result, err := flowEngine.Execute(map[string]any{})
	require.Error(t, err)
	require.NotContains(t, logs.String(), "SSE node execution failed")
	terminalRecords := 0
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record["message"] == "Node execution failed" {
			terminalRecords++
			require.Equal(t, "error", record["level"])
			require.Equal(t, "stream", record["nodeID"])
			require.Equal(t, "logging", record["flowName"])
		}
	}
	require.Equal(t, 1, terminalRecords)
	sseResult, ok := spi.As[*node.SseExecutionResult](result.ExecutionResults["stream"])
	require.True(t, ok)
	require.Equal(t, "SSE_FAILED", *sseResult.ErrorCode)
}
