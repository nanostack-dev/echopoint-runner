// Package flow is the parsed graph: pure data, no behavior. The engine decodes
// each node's Raw config via the registry and schedules the nodes; flow itself
// imports neither node nor engine, so the dependency graph stays acyclic.
package flow

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// CurrentSchemaVersion is the flow-definition schema this runner speaks. It is
// stamped on flows so a breaking wire-format change can be versioned: bump this
// on such a change, migrate older stored flows up to it, and the runner rejects
// any flow declaring a newer version than it understands. Version 1 is the flat
// node format (fields at the node top level, *_ms unit-suffixed durations); the
// pre-versioning "data"-envelope format is unversioned legacy and must be
// migrated before this runner will accept it.
const CurrentSchemaVersion = 1

// Flow is a DAG of nodes plus its initial inputs.
type Flow struct {
	Name          string
	SchemaVersion int
	Nodes         []Node
	Edges         []Edge
	Inputs        value.Map
}

// Node is a raw node definition: its id, kind, run-phase, and undecoded config.
// The engine turns Raw into a node.Bound via the registry. RunWhen is lifted here
// at parse time so the scheduler needn't re-unmarshal it per node.
type Node struct {
	ID      string
	Kind    spi.Kind
	RunWhen spi.RunWhen
	Raw     json.RawMessage
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
		Name          string            `json:"name"`
		SchemaVersion int               `json:"schema_version"`
		Nodes         []json.RawMessage `json:"nodes"`
		Edges         []struct {
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"edges"`
		Inputs map[string]any `json:"inputs"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Flow{}, fmt.Errorf("parse flow: %w", err)
	}

	version := raw.SchemaVersion
	if version == 0 {
		version = CurrentSchemaVersion // unstamped flows are authored against the current schema
	}
	f := Flow{Name: raw.Name, SchemaVersion: version, Inputs: toMap(raw.Inputs)}
	for _, rn := range raw.Nodes {
		var head struct {
			ID      string      `json:"id"`
			Type    spi.Kind    `json:"type"`
			RunWhen spi.RunWhen `json:"run_when"`
		}
		if err := json.Unmarshal(rn, &head); err != nil {
			return Flow{}, fmt.Errorf("parse node head: %w", err)
		}
		if head.RunWhen == "" {
			head.RunWhen = spi.RunWhenOnSuccess
		}
		f.Nodes = append(f.Nodes, Node{ID: head.ID, Kind: head.Type, RunWhen: head.RunWhen, Raw: rn})
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
	if f.SchemaVersion > CurrentSchemaVersion {
		return fmt.Errorf(
			"flow schema version %d is newer than this runner supports (%d); upgrade the runner",
			f.SchemaVersion, CurrentSchemaVersion)
	}
	ids := make(map[string]bool, len(f.Nodes))
	for _, n := range f.Nodes {
		if n.ID == "" {
			return errors.New("node with empty id")
		}
		if ids[n.ID] {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
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
