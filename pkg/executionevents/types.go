package executionevents

type Type string

const (
	FlowStarted   Type = "flow.started"
	NodeStarted   Type = "node.started"
	NodeCompleted Type = "node.completed"
	NodeFailed    Type = "node.failed"
)

func AllTypes() []Type {
	return []Type{
		FlowStarted,
		NodeStarted,
		NodeCompleted,
		NodeFailed,
	}
}
