package engine_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	internalLogger "github.com/nanostack-dev/echopoint-runner/internal/logger"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/rs/zerolog"
)

const benchmarkBranchCount = 6

func BenchmarkParallelHTTPFlow(b *testing.B) {
	baseURL := os.Getenv("ECHOPOINT_BENCH_BASE_URL")
	if baseURL == "" {
		b.Skip("ECHOPOINT_BENCH_BASE_URL is not set")
	}

	internalLogger.InitLogger(zerolog.Disabled, internalLogger.JSON)

	flowJSON := buildBenchmarkFlowJSON(b, baseURL, benchmarkBranchCount)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		flowDef, err := flow.ParseFromJSON(flowJSON)
		if err != nil {
			b.Fatalf("parse benchmark flow: %v", err)
		}

		flowEngine, err := engine.NewFlowEngine(*flowDef, nil)
		if err != nil {
			b.Fatalf("create flow engine: %v", err)
		}

		result, err := flowEngine.Execute(flowDef.InitialInputs)
		if err != nil {
			b.Fatalf("execute flow: %v", err)
		}
		if !result.Success {
			b.Fatalf("benchmark flow did not succeed")
		}
		if metric := numericValue(result.FinalOutputs["join.statusCode"]); metric != 200 {
			b.Fatalf("unexpected join status code: %v", result.FinalOutputs["join.statusCode"])
		}
	}
}

func buildBenchmarkFlowJSON(b *testing.B, baseURL string, branchCount int) []byte {
	b.Helper()

	nodes := make([]map[string]interface{}, 0, branchCount+1)
	edges := make([]map[string]interface{}, 0, branchCount)
	joinHeaders := make(map[string]string, branchCount)

	for i := 1; i <= branchCount; i++ {
		nodeID := fmt.Sprintf("branch-%d", i)
		nodes = append(nodes, map[string]interface{}{
			"id":         nodeID,
			"type":       "request",
			"assertions": []interface{}{},
			"outputs": []map[string]interface{}{
				{
					"name": "statusCode",
					"extractor": map[string]interface{}{
						"type": "statusCode",
					},
				},
			},
			"data": map[string]interface{}{
				"method":  "GET",
				"url":     fmt.Sprintf("{{apiUrl}}/bench/slow/%d", i),
				"timeout": 5000,
			},
		})

		joinHeaders[fmt.Sprintf("X-Branch-%d", i)] = fmt.Sprintf("{{%s.statusCode}}", nodeID)
		edges = append(edges, map[string]interface{}{
			"id":     fmt.Sprintf("%s-to-join", nodeID),
			"source": nodeID,
			"target": "join",
			"type":   "success",
		})
	}

	nodes = append(nodes, map[string]interface{}{
		"id":         "join",
		"type":       "request",
		"assertions": []interface{}{},
		"outputs": []map[string]interface{}{
			{
				"name": "statusCode",
				"extractor": map[string]interface{}{
					"type": "statusCode",
				},
			},
		},
		"data": map[string]interface{}{
			"method":  "GET",
			"url":     "{{apiUrl}}/bench/join",
			"headers": joinHeaders,
			"timeout": 5000,
		},
	})

	flowMap := map[string]interface{}{
		"name":        "Autoresearch Parallel HTTP Flow",
		"description": "Fan-out/fan-in request flow for runner autoresearch.",
		"version":     "1.0",
		"initialInputs": map[string]interface{}{
			"apiUrl": baseURL,
		},
		"nodes": nodes,
		"edges": edges,
	}

	flowJSON, err := json.Marshal(flowMap)
	if err != nil {
		b.Fatalf("marshal benchmark flow: %v", err)
	}

	return flowJSON
}

func numericValue(value interface{}) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	default:
		return -1
	}
}
