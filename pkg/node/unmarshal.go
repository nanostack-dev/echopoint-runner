package node

import (
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// UnmarshalNode unmarshals JSON into the appropriate typed node via the node-kind
// registry (see registry.go). Adding a node type is one RegisterNodeKind call, not
// a new case here.
func UnmarshalNode(data []byte) (AnyNode, error) {
	var peek struct {
		Type spi.Kind `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("failed to peek node type: %w", err)
	}

	kind, ok := nodeKinds[peek.Type]
	if !ok {
		return nil, fmt.Errorf("unknown node type: %s", peek.Type)
	}
	return kind.decode(data)
}
