package node

import (
	"encoding/json"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Decoder builds a typed node from its raw JSON.
type Decoder func([]byte) (AnyNode, error)

// SkippedResultFactory builds the kind's skipped execution result from a shared
// base, so the engine doesn't switch on node type to construct skips.
type SkippedResultFactory func(base spi.BaseExecutionResult) spi.AnyResult

type nodeKind struct {
	decode     Decoder
	newSkipped SkippedResultFactory
}

// nodeKinds is the node-kind registry. Built-ins register below; a new kind is
// one RegisterNodeKind call (decoder + skipped factory) plus its Execute — no
// edits to UnmarshalNode or the engine's skip logic.
//
//nolint:gochecknoglobals // immutable-after-init node-kind registry
var nodeKinds = map[spi.Kind]nodeKind{}

// RegisterNodeKind registers how to decode a node type and build its skipped
// result. Call from an init().
func RegisterNodeKind(nodeType spi.Kind, decode Decoder, newSkipped SkippedResultFactory) {
	nodeKinds[nodeType] = nodeKind{decode: decode, newSkipped: newSkipped}
}

// NewSkippedResult builds the skipped result for nodeType via the registry,
// reporting false when the type is unregistered.
func NewSkippedResult(nodeType spi.Kind, base spi.BaseExecutionResult) (spi.AnyResult, bool) {
	kind, ok := nodeKinds[nodeType]
	if !ok {
		return nil, false
	}
	return kind.newSkipped(base), true
}

func applyRunWhenDefault(base *BaseNode) {
	if base.RunWhen == "" {
		base.RunWhen = spi.RunWhenOnSuccess
	}
}

//nolint:gochecknoinits // register built-in node kinds at package load
func init() {
	registerCoreNodeKinds()
	registerFlowNodeKinds()
}

// registerCoreNodeKinds registers the original primitive node kinds.
func registerCoreNodeKinds() {
	RegisterNodeKind(spi.KindRequest,
		func(data []byte) (AnyNode, error) {
			var n RequestNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &RequestExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(spi.KindDelay,
		func(data []byte) (AnyNode, error) {
			var n DelayNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &DelayExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(spi.KindModule,
		func(data []byte) (AnyNode, error) {
			var n ModuleNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &ModuleExecutionResult{BaseExecutionResult: base}
		},
	)
}

// registerFlowNodeKinds registers the flow-authoring node kinds.
func registerFlowNodeKinds() {
	RegisterNodeKind(spi.KindSetVariable,
		func(data []byte) (AnyNode, error) {
			var n SetVariableNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &SetVariableExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(spi.KindLoop,
		func(data []byte) (AnyNode, error) {
			var n LoopNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &LoopExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(spi.KindPoll,
		func(data []byte) (AnyNode, error) {
			var n PollNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &PollExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(spi.KindAssert,
		func(data []byte) (AnyNode, error) {
			var n AssertNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &AssertExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(spi.KindBranch,
		func(data []byte) (AnyNode, error) {
			var n BranchNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &BranchExecutionResult{BaseExecutionResult: base}
		},
	)
	RegisterNodeKind(spi.KindSse,
		func(data []byte) (AnyNode, error) {
			var n SseNode
			if err := json.Unmarshal(data, &n); err != nil {
				return nil, err
			}
			applyRunWhenDefault(&n.BaseNode)
			return &n, nil
		},
		func(base spi.BaseExecutionResult) spi.AnyResult {
			return &SseExecutionResult{BaseExecutionResult: base}
		},
	)
}
