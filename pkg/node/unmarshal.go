package node

import (
	"encoding/json"
	"fmt"
)

// UnmarshalNode unmarshals JSON into the appropriate typed node based on the type field.
func UnmarshalNode(data []byte) (AnyNode, error) {
	var peek struct {
		Type Type `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("failed to peek node type: %w", err)
	}

	switch peek.Type {
	case TypeRequest:
		var node RequestNode
		if err := json.Unmarshal(data, &node); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request node: %w", err)
		}
		return &node, nil
	case TypeDelay:
		var node DelayNode
		if err := json.Unmarshal(data, &node); err != nil {
			return nil, fmt.Errorf("failed to unmarshal delay node: %w", err)
		}
		return &node, nil
	case TypeDebug:
		var node DebugNode
		if err := json.Unmarshal(data, &node); err != nil {
			return nil, fmt.Errorf("failed to unmarshal debug node: %w", err)
		}
		return &node, nil
	default:
		return nil, fmt.Errorf("unknown node type: %s", peek.Type)
	}
}
