package executionevents

import "github.com/nanostack-dev/echopoint-runner/pkg/spi"

// Type is re-exported from spi (the L0 contract). Alias kept for back-compat.
type Type = spi.EventType

// Execution/progress event types (re-exported from spi).
const (
	FlowStarted   = spi.EventFlowStarted
	NodeStarted   = spi.EventNodeStarted
	NodeCompleted = spi.EventNodeCompleted
	NodeFailed    = spi.EventNodeFailed
	FlowCompleted = spi.EventFlowCompleted
	FlowFailed    = spi.EventFlowFailed
)

func ProgressTypes() []Type {
	return []Type{
		FlowStarted,
		NodeStarted,
		NodeCompleted,
		NodeFailed,
	}
}

func TerminalTypes() []Type {
	return []Type{
		FlowCompleted,
		FlowFailed,
	}
}

func AllTypes() []Type {
	all := make([]Type, 0, len(ProgressTypes())+len(TerminalTypes()))
	all = append(all, ProgressTypes()...)
	all = append(all, TerminalTypes()...)
	return all
}
