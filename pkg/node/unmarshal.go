package node

import (
	"encoding/json"
	"errors"
	"fmt"
)

// UnmarshalNode unmarshals JSON into the appropriate typed node based on the type field.
func UnmarshalNode(data []byte) (AnyNode, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse node JSON: %w", err)
	}

	nodeTypeStr, ok := raw["type"].(string)
	if !ok {
		return nil, errors.New("missing or invalid node type")
	}
	nodeType := Type(nodeTypeStr)

	// BRIDGE LOGIC: Hoist nested fields from 'data' to root if present.
	// The API contract (OpenAPI) nests these inside 'data', but the Go engine
	// expects them at the root level of the node for internal processing.
	normalizeNodeStructure(raw)

	// Re-serialize the normalized map
	normalizedData, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to re-serialize normalized node data: %w", err)
	}

	switch nodeType {
	case TypeRequest:
		var reqNode RequestNode
		if err = json.Unmarshal(normalizedData, &reqNode); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request node: %w", err)
		}
		return &reqNode, nil
	case TypeDelay:
		var delayNode DelayNode
		if err = json.Unmarshal(normalizedData, &delayNode); err != nil {
			return nil, fmt.Errorf("failed to unmarshal delay node: %w", err)
		}
		return &delayNode, nil
	default:
		return nil, fmt.Errorf("unknown node type: %s", nodeType)
	}
}

// normalizeNodeStructure hoists nested fields from 'data' to root if present.
func normalizeNodeStructure(raw map[string]interface{}) {
	innerData, hasData := raw["data"].(map[string]interface{})
	if !hasData {
		return
	}

	// Hoist outputs if root is empty but nested has data
	if _, hasRoot := raw["outputs"]; !hasRoot {
		if nested, hasNested := innerData["outputs"]; hasNested {
			raw["outputs"] = nested
		}
	}

	// Hoist assertions if root is empty but nested has data
	if _, hasRoot := raw["assertions"]; !hasRoot {
		if nested, hasNested := innerData["assertions"]; hasNested {
			raw["assertions"] = nested
		}
	}
}
