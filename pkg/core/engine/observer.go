package engine

import (
	"github.com/nanostack-dev/echopoint-runner/pkg/core/result"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Event is an execution event emitted during a top-level flow run. Node carries
// the node's result for node.* events; Flow carries the flow result for flow.*
// events.
//
// A node that runs a sub-flow (module/loop/poll body) emits as one node event —
// its inner nodes are silent. This keeps the wire flat and re-running bodies
// (poll/loop) from storming events. If nested progress is ever needed, add depth
// to Event rather than un-silencing the recursion.
type Event struct {
	Type   spi.EventType
	NodeID string
	Node   *result.NodeResult
	Flow   *result.FlowResult
}

// Observer receives execution events (progress streaming). It must be safe to
// call synchronously from the engine.
type Observer func(Event)

// emit sends an event to obs when one is bound (nil for sub-flow runs).
func emit(obs Observer, ev Event) {
	if obs != nil {
		obs(ev)
	}
}
