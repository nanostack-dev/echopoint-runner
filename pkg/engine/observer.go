package engine

import (
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

type FlowStartedEvent struct {
	FlowName  string
	StartedAt time.Time
}

type NodeStartedEvent struct {
	NodeID      string
	DisplayName string
	NodeType    node.Type
	StartedAt   time.Time
}

type NodeFinishedEvent struct {
	NodeID      string
	DisplayName string
	NodeType    node.Type
	StartedAt   time.Time
	FinishedAt  time.Time
	DurationMs  int64
	Result      node.AnyExecutionResult
}

type FlowFinishedEvent struct {
	FlowName   string
	StartedAt  time.Time
	FinishedAt time.Time
	DurationMs int64
	Result     *node.FlowExecutionResult
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

type MultiObserver []ExecutionObserver

func (m MultiObserver) FlowStarted(evt FlowStartedEvent) {
	for _, observer := range m {
		if observer == nil {
			continue
		}
		observer.FlowStarted(evt)
	}
}

func (m MultiObserver) NodeStarted(evt NodeStartedEvent) {
	for _, observer := range m {
		if observer == nil {
			continue
		}
		observer.NodeStarted(evt)
	}
}

func (m MultiObserver) NodeFinished(evt NodeFinishedEvent) {
	for _, observer := range m {
		if observer == nil {
			continue
		}
		observer.NodeFinished(evt)
	}
}

func (m MultiObserver) FlowFinished(evt FlowFinishedEvent) {
	for _, observer := range m {
		if observer == nil {
			continue
		}
		observer.FlowFinished(evt)
	}
}
