package flow

import (
	"encoding/json"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/edge"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
)

type Flow struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Nodes       []node.AnyNode `json:"-"`
	Edges       []edge.Edge    `json:"edges"`
	Version     string         `json:"version"`
}

func ParseFromMap(data map[string]interface{}) (*Flow, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return ParseFromJSON(jsonData)
}
func ParseFromJSON(data []byte) (*Flow, error) {
	var raw struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Nodes       []json.RawMessage `json:"nodes"`
		Edges       []edge.Edge       `json:"edges"`
		Version     string            `json:"version"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	// Convert raw nodes to typed nodes
	nodes := make([]node.AnyNode, len(raw.Nodes))
	for i, rawNode := range raw.Nodes {
		typedNode, err := node.UnmarshalNode(rawNode)
		if err != nil {
			return nil, err
		}
		nodes[i] = typedNode
	}

	return &Flow{
		Name:        raw.Name,
		Description: raw.Description,
		Nodes:       nodes,
		Edges:       raw.Edges,
		Version:     raw.Version,
	}, nil
}
