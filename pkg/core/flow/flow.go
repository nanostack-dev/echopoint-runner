// Package flow is the parsed graph: pure data, no behavior. The engine decodes
// each node's Raw config via the registry and schedules the nodes; flow itself
// imports neither node nor engine, so the dependency graph stays acyclic.
package flow

import (
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Flow is a DAG of nodes plus its initial inputs.
type Flow struct {
	Name   string
	Nodes  []Node
	Edges  []Edge
	Inputs value.Map
}

// Node is a raw node definition: its id, kind, and undecoded config. The engine
// turns Raw into a node.Bound via the registry.
type Node struct {
	ID   string
	Kind spi.Kind
	Raw  json.RawMessage
}

// Edge is a directed dependency: To runs after From.
type Edge struct {
	From string
	To   string
}

// Parse reads a flow definition from JSON. Each node's full object is retained
// as Raw so the registry can decode it into the node's typed config.
func Parse(b []byte) (Flow, error) {
	var raw struct {
		Name  string            `json:"name"`
		Nodes []json.RawMessage `json:"nodes"`
		Edges []struct {
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"edges"`
		Inputs map[string]any `json:"inputs"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Flow{}, fmt.Errorf("parse flow: %w", err)
	}

	f := Flow{Name: raw.Name, Inputs: toMap(raw.Inputs)}
	for _, rn := range raw.Nodes {
		var head struct {
			ID   string   `json:"id"`
			Type spi.Kind `json:"type"`
		}
		if err := json.Unmarshal(rn, &head); err != nil {
			return Flow{}, fmt.Errorf("parse node head: %w", err)
		}
		f.Nodes = append(f.Nodes, Node{ID: head.ID, Kind: head.Type, Raw: rn})
	}
	for _, e := range raw.Edges {
		f.Edges = append(f.Edges, Edge{From: e.Source, To: e.Target})
	}
	return f, nil
}

// Validate checks structural invariants before execution: every edge references
// a declared node. Node-shape validation (route targets, referenced child flows)
// is done by the engine via node capabilities, so flow stays free of node-type
// knowledge.
func Validate(f Flow) error {
	ids := make(map[string]bool, len(f.Nodes))
	for _, n := range f.Nodes {
		ids[n.ID] = true
	}
	for _, e := range f.Edges {
		if !ids[e.From] {
			return fmt.Errorf("edge source %q is not a declared node", e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("edge target %q is not a declared node", e.To)
		}
	}
	return nil
}

func toMap(m map[string]any) value.Map {
	if m == nil {
		return nil
	}
	out := make(value.Map, len(m))
	for k, v := range m {
		out[k] = value.Of(v)
	}
	return out
}
