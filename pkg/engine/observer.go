package engine

import (
	"sync"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type FlowStartedEvent struct {
	FlowName  string
	StartedAt time.Time
}

type NodeStartedEvent struct {
	NodeID      string
	DisplayName string
	NodeType    spi.Kind
	StartedAt   time.Time
}

type NodeFinishedEvent struct {
	NodeID      string
	DisplayName string
	NodeType    spi.Kind
	StartedAt   time.Time
	FinishedAt  time.Time
	DurationMs  int64
	Result      spi.AnyResult
}

type FlowFinishedEvent struct {
	FlowName   string
	StartedAt  time.Time
	FinishedAt time.Time
	DurationMs int64
	Result     *spi.FlowExecutionResult
}

type ExecutionObserver interface {
	FlowStarted(evt FlowStartedEvent)
	NodeStarted(evt NodeStartedEvent)
	NodeFinished(evt NodeFinishedEvent)
	FlowFinished(evt FlowFinishedEvent)
}

// finishNode notifies the observer that n reached a terminal state. Every way a
// node can finish — executed, failed, or skipped without running — reports the
// same identity fields, so they are read off the node here rather than at each
// call site.
//
// DurationMs is derived from the span the caller passes rather than measured
// separately: the two cannot then disagree about how long the node took.
func (engine *FlowEngine) finishNode(
	n node.AnyNode,
	startedAt, finishedAt time.Time,
	result spi.AnyResult,
) {
	engine.observer.NodeFinished(NodeFinishedEvent{
		NodeID:      n.GetID(),
		DisplayName: n.GetDisplayName(),
		NodeType:    n.GetType(),
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
		DurationMs:  finishedAt.Sub(startedAt).Milliseconds(),
		Result:      result,
	})
}

// finishFlow notifies the observer that the run is over, whatever the outcome.
// It reads the duration off the result, which every exit path has already
// stamped, so the event cannot report a different number than the result does.
func (engine *FlowEngine) finishFlow(result *spi.FlowExecutionResult, startedAt time.Time) {
	engine.observer.FlowFinished(FlowFinishedEvent{
		FlowName:   engine.flow.Name,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
		DurationMs: result.DurationMS,
		Result:     result,
	})
}

type NoopObserver struct{}

func (NoopObserver) FlowStarted(FlowStartedEvent)   {}
func (NoopObserver) NodeStarted(NodeStartedEvent)   {}
func (NoopObserver) NodeFinished(NodeFinishedEvent) {}
func (NoopObserver) FlowFinished(FlowFinishedEvent) {}

type synchronizedObserver struct {
	inner ExecutionObserver
	mu    sync.Mutex
}

func ensureSynchronizedObserver(observer ExecutionObserver) ExecutionObserver {
	if observer == nil {
		return NoopObserver{}
	}
	if synchronized, ok := observer.(*synchronizedObserver); ok {
		return synchronized
	}
	return &synchronizedObserver{inner: observer}
}

func (s *synchronizedObserver) FlowStarted(evt FlowStartedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.FlowStarted(evt)
}

func (s *synchronizedObserver) NodeStarted(evt NodeStartedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.NodeStarted(evt)
}

func (s *synchronizedObserver) NodeFinished(evt NodeFinishedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.NodeFinished(evt)
}

func (s *synchronizedObserver) FlowFinished(evt FlowFinishedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.FlowFinished(evt)
}
