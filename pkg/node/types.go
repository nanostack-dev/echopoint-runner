package node

type AnyNode interface {
	GetID() string
	GetType() Type
	Execute() (bool, error)
}

type TypeNode[T any] interface {
	AnyNode
	GetData() T
}

type Type string

const (
	TypeRequest Type = "request"
	TypeDelay   Type = "delay"
)
