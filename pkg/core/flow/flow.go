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
// declared nodes, and every branch case/default target is a real successor edge
// of the branch. It does not execute anything (no side effects).
func Validate(f Flow) error {
	ids := make(map[string]bool, len(f.Nodes))
	for _, n := range f.Nodes {
		ids[n.ID] = true
	}
	succ := make(map[string]map[string]bool, len(f.Nodes))
	for _, e := range f.Edges {
		if !ids[e.From] {
			return fmt.Errorf("edge source %q is not a declared node", e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("edge target %q is not a declared node", e.To)
		}
		if succ[e.From] == nil {
			succ[e.From] = map[string]bool{}
		}
		succ[e.From][e.To] = true
	}
	for _, n := range f.Nodes {
		if n.Kind != spi.KindBranch {
			continue
		}
		if err := validateBranchTargets(n, succ[n.ID]); err != nil {
			return err
		}
	}
	return nil
}

func validateBranchTargets(n Node, targets map[string]bool) error {
	var cfg struct {
		Cases []struct {
			Target string `json:"target"`
		} `json:"cases"`
		Default string `json:"default"`
	}
	if err := json.Unmarshal(n.Raw, &cfg); err != nil {
		return fmt.Errorf("branch %q: %w", n.ID, err)
	}
	check := func(target string) error {
		if target != "" && !targets[target] {
			return fmt.Errorf("branch %q routes to %q but has no edge to it", n.ID, target)
		}
		return nil
	}
	for _, c := range cfg.Cases {
		if err := check(c.Target); err != nil {
			return err
		}
	}
	return check(cfg.Default)
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
