package engine

import (
	"sync"
	"time"

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
