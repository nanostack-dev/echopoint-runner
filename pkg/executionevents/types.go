package executionevents

import "github.com/nanostack-dev/echopoint-runner/pkg/spi"

func ProgressTypes() []spi.EventType {
	return []spi.EventType{
		spi.EventFlowStarted,
		spi.EventNodeStarted,
		spi.EventNodeCompleted,
		spi.EventNodeFailed,
	}
}

func TerminalTypes() []spi.EventType {
	return []spi.EventType{
		spi.EventFlowCompleted,
		spi.EventFlowFailed,
	}
}

func AllTypes() []spi.EventType {
	all := make([]spi.EventType, 0, len(ProgressTypes())+len(TerminalTypes()))
	all = append(all, ProgressTypes()...)
	all = append(all, TerminalTypes()...)
	return all
}
