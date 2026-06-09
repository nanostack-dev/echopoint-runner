package spi

// EventType identifies a runner execution/progress event on the wire. The
// helper sets (progress vs terminal) live with the consumers in
// pkg/executionevents; this package owns only the wire identifiers.
type EventType string

// Execution/progress event types.
const (
	EventFlowStarted   EventType = "flow.started"
	EventNodeStarted   EventType = "node.started"
	EventNodeCompleted EventType = "node.completed"
	EventNodeFailed    EventType = "node.failed"
	EventFlowCompleted EventType = "flow.completed"
	EventFlowFailed    EventType = "flow.failed"
)
