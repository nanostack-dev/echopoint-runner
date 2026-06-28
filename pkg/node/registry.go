package node

import "encoding/json"

// Decoder builds a typed node from its raw JSON.
type Decoder func([]byte) (AnyNode, error)

// SkippedResultFactory builds the kind's skipped execution result from a shared
// base, so the engine doesn't switch on node type to construct skips.
type SkippedResultFactory func(base BaseExecutionResult) AnyExecutionResult

type nodeKind struct {
	decode     Decoder
	newSkipped SkippedResultFactory
}

// nodeKinds is the node-kind registry. Built-ins register below; a new kind is
// one RegisterNodeKind call (decoder + skipped factory) plus its Execute — no
// edits to UnmarshalNode or the engine's skip logic.
//
//nolint:gochecknoglobals // immutable-after-init node-kind registry
var nodeKinds = map[Type]nodeKind{}

// RegisterNodeKind registers how to decode a node type and build its skipped
// result. Call from an init().
func RegisterNodeKind(nodeType Type, decode Decoder, newSkipped SkippedResultFactory) {
	nodeKinds[nodeType] = nodeKind{decode: decode, newSkipped: newSkipped}
}

// NewSkippedResult builds the skipped result for nodeType via the registry,
// reporting false when the type is unregistered.
func NewSkippedResult(nodeType Type, base BaseExecutionResult) (AnyExecutionResult, bool) {
	kind, ok := nodeKinds[nodeType]
	if !ok {
		return nil, false
	}
	return kind.newSkipped(base), true
}

func applyRunWhenDefault(base *BaseNode) {
	if base.RunWhen == "" {
		base.RunWhen = RunWhenOnSuccess
	}
}

//nolint:gochecknoinits // register built-in node kinds at package load
func init() {
	RegisterNodeKind(TypeRequest,
		func(data []byte) (AnyNode, error) {
			var n RequestNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base BaseExecutionResult) AnyExecutionResult {
			return &RequestExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(TypeDelay,
		func(data []byte) (AnyNode, error) {
			var n DelayNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base BaseExecutionResult) AnyExecutionResult {
			return &DelayExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(TypeModule,
		func(data []byte) (AnyNode, error) {
			var n ModuleNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base BaseExecutionResult) AnyExecutionResult {
			return &ModuleExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(TypeSse,
		func(data []byte) (AnyNode, error) {
			var n SseNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base BaseExecutionResult) AnyExecutionResult {
			return &SseExecutionResult{BaseExecutionResult: base}
		},
	)
}
