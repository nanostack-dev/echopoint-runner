package engine

import (
	"github.com/nanostack-dev/echopoint-runner/pkg/core/result"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Event is an execution event emitted during a top-level flow run. Node carries
// the node's result for node.* events; Flow carries the flow result for flow.*
// events. Sub-flow (module/loop/poll body) runs are silent.
type Event struct {
	Type   spi.EventType
	NodeID string
	Node   *result.NodeResult
	Flow   *result.FlowResult
}

// Observer receives execution events (progress streaming). It must be safe to
// call synchronously from the engine.
type Observer func(Event)

func (e *Engine) emit(on bool, ev Event) {
	if on && e.observer != nil {
		e.observer(ev)
	}
}
