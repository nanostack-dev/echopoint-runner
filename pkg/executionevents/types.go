package executionevents

type Type string

const (
	FlowStarted   Type = "flow.started"
	NodeStarted   Type = "node.started"
	NodeCompleted Type = "node.completed"
	NodeFailed    Type = "node.failed"
	FlowCompleted Type = "flow.completed"
	FlowFailed    Type = "flow.failed"
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
