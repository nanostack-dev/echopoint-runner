package flow

import (
	"encoding/json"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/edge"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/node"
)

type Flow struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Version       string                 `json:"version"`
	Nodes         []node.AnyNode         `json:"-"`
	Edges         []edge.Edge            `json:"edges"`
	InitialInputs map[string]interface{} `json:"initialInputs"`
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
		Name          string                 `json:"name"`
		Description   string                 `json:"description"`
		Version       string                 `json:"version"`
		Nodes         []json.RawMessage      `json:"nodes"`
		Edges         []edge.Edge            `json:"edges"`
		InitialInputs map[string]interface{} `json:"initialInputs"`
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

	// Ensure InitialInputs is initialized
	if raw.InitialInputs == nil {
		raw.InitialInputs = make(map[string]interface{})
	}

	return &Flow{
		Name:          raw.Name,
		Description:   raw.Description,
		Version:       raw.Version,
		Nodes:         nodes,
		Edges:         raw.Edges,
		InitialInputs: raw.InitialInputs,
	}, nil
}
