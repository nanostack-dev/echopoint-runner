//nolint:testpackage // white-box: drives the unexported job executor like the claim loop does
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	configpkg "github.com/nanostack-dev/echopoint-runner/internal/config"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
)

const mebibyte = 1 << 20

// largeResponseLoad reproduces the 2026-10-14 dev OOM: concurrent flows, each
// sending parallel GETs to an endpoint that answers 10 MiB of JSON, through the
// self-hosted path (runtime, runner.Run, redaction, progress events and the
// completion).
type largeResponseLoad struct {
	flows        int
	nodesPerFlow int
	bodyBytes    int
	// assertion is the assertion each request node runs.
	assertion string
}

const (
	statusAssertion = `{"extractor_type": "status_code", "operator_type": "equals", "operator_data": {"value": 200}}`
	// bodyAssertion reads the body, so the node keeps and parses all of it.
	bodyAssertion = `{"extractor_type": "json_path", "extractor_data": {"path": "$.kb"}, ` +
		`"operator_type": "equals", "operator_data": {"value": 10240}}`
)

type memoryReport struct {
	responses int
	// peakHeapInuse is the highest HeapInuse sampled during the run, above the
	// baseline taken before it.
	peakHeapInuse uint64
	// retainedHeap is the live heap after a GC once every job finished, while
	// their results are still held, above the baseline.
	retainedHeap uint64
	totalAlloc   uint64
	completed    int
}

func (m memoryReport) perResponse(bytes uint64) float64 {
	return float64(bytes) / float64(m.responses)
}

func (m memoryReport) String() string {
	return fmt.Sprintf(
		"%d responses: peak HeapInuse %.0f MiB (%.1f MiB/response), retained %.1f MiB (%.2f MiB/response), TotalAlloc %.0f MiB",
		m.responses,
		float64(m.peakHeapInuse)/mebibyte,
		m.perResponse(m.peakHeapInuse)/mebibyte,
		float64(m.retainedHeap)/mebibyte,
		m.perResponse(m.retainedHeap)/mebibyte,
		float64(m.totalAlloc)/mebibyte,
	)
}

// largeJSONBody is the mock API's /v1/large?kb=10240 answer: exactly size bytes.
func largeJSONBody(size int) []byte {
	prefix := `{"kb":10240,"clamped":false,"padding":"`
	suffix := `"}`
	return []byte(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
}

func largeResponseJob(targetURL string, nodes int, assertion string) *controlplane.ClaimedJob {
	nodeDefs := make([]string, nodes)
	for i := range nodeDefs {
		nodeDefs[i] = fmt.Sprintf(`{
			"id": "large_%d", "type": "request",
			"data": {"method": "GET", "url": "%s/v1/large?kb=10240", "headers": {"Authorization": "Basic {{token}}"}},
			"inputs": ["token"],
			"assertions": [%s]
		}`, i, targetURL, assertion)
	}
	return &controlplane.ClaimedJob{
		JobID:       uuid.New(),
		ExecutionID: uuid.New(),
		FlowDefinition: json.RawMessage(`{"name": "Large", "version": "1.0", "edges": [], "nodes": [` +
			strings.Join(nodeDefs, ",") + `]}`),
		Inputs:          map[string]any{"token": "dXNlcjpzZWNyZXQtcGFzc3dvcmQ="},
		SecretInputKeys: []string{"token"},
	}
}

// fakeControlPlane accepts every progress batch and completion, reading each
// body like the real control plane would.
func fakeControlPlane(completed *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/events"):
			var batch controlplane.SendJobEventsRequest
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var last int64
			for _, event := range batch.Events {
				last = max(last, event.Sequence)
			}
			_ = json.NewEncoder(w).Encode(controlplane.SendJobEventsResponse{LastAcceptedSequence: last})
		case strings.HasSuffix(r.URL.Path, "/complete"):
			_, _ = io.Copy(io.Discard, r.Body)
			completed.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
}

// sampleHeap records the highest HeapInuse until stop is closed.
func sampleHeap(stop <-chan struct{}, peak *uint64, done chan<- struct{}) {
	defer close(done)
	var stats runtime.MemStats
	for {
		runtime.ReadMemStats(&stats)
		*peak = max(*peak, stats.HeapInuse)
		select {
		case <-stop:
			return
		case <-time.After(time.Millisecond):
		}
	}
}

func measureLargeResponses(
	tb testing.TB,
	load largeResponseLoad,
	config func(baseURL string) configpkg.Config,
) memoryReport {
	tb.Helper()
	body := largeJSONBody(load.bodyBytes)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	defer target.Close()
	var completed atomic.Int32
	controlPlane := fakeControlPlane(&completed)
	defer controlPlane.Close()

	cfg := config(controlPlane.URL)
	cfg.MaxParallelFlows = load.flows
	r := newRuntime(cfg, controlplane.NewRunnerClient(controlplane.Config{
		BaseURL: controlPlane.URL, RequestTimeout: cfg.RequestTimeout,
	}))
	jobs := make([]*controlplane.ClaimedJob, load.flows)
	for i := range jobs {
		jobs[i] = largeResponseJob(target.URL, load.nodesPerFlow, load.assertion)
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak uint64
	stop, sampled := make(chan struct{}), make(chan struct{})
	go sampleHeap(stop, &peak, sampled)

	outcomes := make([]Outcome, len(jobs))
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Go(func() {
			outcome, err := r.executeClaimedJob(context.Background(), &activeJob{job: job, startedAt: time.Now()})
			assert.NoError(tb, err)
			outcomes[i] = outcome
		})
	}
	wg.Wait()
	close(stop)
	<-sampled

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	for _, outcome := range outcomes {
		assert.Equal(tb, jobStatusCompleted, outcome.Status, "a job failed: %v", outcome.ErrorMessage)
	}
	runtime.KeepAlive(outcomes)

	return memoryReport{
		responses:     load.flows * load.nodesPerFlow,
		peakHeapInuse: positiveDelta(peak, before.HeapInuse),
		retainedHeap:  positiveDelta(after.HeapAlloc, before.HeapAlloc),
		totalAlloc:    after.TotalAlloc - before.TotalAlloc,
		completed:     int(completed.Load()),
	}
}

func positiveDelta(value, baseline uint64) uint64 {
	if value < baseline {
		return 0
	}
	return value - baseline
}

func largeResponseConfig(baseURL string) configpkg.Config {
	config := testConfig(baseURL)
	config.RequestTimeout = 30 * time.Second
	config.MaxResponseBytes = 10 * mebibyte
	config.MaxBufferedResponseBytes = configpkg.DefaultMaxBufferedResponseBytes
	return config
}

// The 2026-10-14 dev repro: 3 parallel flows of 4 parallel 10 MiB GETs each,
// asserting the status only, in a 512 MiB container.
var devOOMLoad = largeResponseLoad{flows: 3, nodesPerFlow: 4, bodyBytes: 10 * mebibyte, assertion: statusAssertion}

func withAssertion(load largeResponseLoad, assertion string) largeResponseLoad {
	load.assertion = assertion
	return load
}

type namedLoad struct {
	name string
	load largeResponseLoad
}

// largeResponseLoads asserts the status only, as the repro did, then reads the
// body, which keeps and parses every response.
var largeResponseLoads = []namedLoad{
	{"status", devOOMLoad},
	{"body", withAssertion(devOOMLoad, bodyAssertion)},
}

func TestConcurrentLargeResponsesStayWithinTheMemoryTarget(t *testing.T) {
	for _, named := range largeResponseLoads {
		load := named.load
		t.Run(named.name, func(t *testing.T) {
			report := measureLargeResponses(t, load, largeResponseConfig)
			t.Log(report)

			assert.Equal(t, load.flows, report.completed)
			assert.Less(t, report.peakHeapInuse, uint64(300*mebibyte), "peak heap: %s", report)
		})
	}
}

func BenchmarkConcurrentLargeResponses(b *testing.B) {
	for _, named := range largeResponseLoads {
		load := named.load
		b.Run(named.name, func(b *testing.B) {
			for b.Loop() {
				report := measureLargeResponses(b, load, largeResponseConfig)
				b.ReportMetric(float64(report.peakHeapInuse)/mebibyte, "peak-heap-MiB")
				b.ReportMetric(report.perResponse(report.peakHeapInuse)/float64(load.bodyBytes), "peak-x-body")
				b.ReportMetric(float64(report.retainedHeap)/mebibyte, "retained-MiB")
				b.ReportMetric(float64(report.totalAlloc)/mebibyte, "alloc-MiB")
			}
		})
	}
}
