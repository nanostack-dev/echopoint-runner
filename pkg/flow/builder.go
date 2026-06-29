package flow

import (
	"github.com/nanostack-dev/echopoint-runner/pkg/edge"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

// Builder constructs a Flow fluently in Go, as an alternative to parsing JSON.
// Use the node constructors in pkg/node (node.NewRequest, node.NewDelay, ...).
//
//	f := flow.NewBuilder("provision").Version("1.0").
//	    Input("baseURL", "https://api.example.com").
//	    Add(node.NewRequest("create").POST("{{baseURL}}/customers")).
//	    Add(node.NewRequest("provision").POST("{{baseURL}}/workspaces")).
//	    Edge("create", "provision").
//	    Build()
type Builder struct {
	flow Flow
}

// NewBuilder starts a flow with the given name (version defaults to "1.0").
func NewBuilder(name string) *Builder {
	return &Builder{flow: Flow{Name: name, Version: "1.0"}}
}

// Version sets the flow version.
func (b *Builder) Version(version string) *Builder {
	b.flow.Version = version
	return b
}

// Input declares an initial input (a default; launch inputs override it).
func (b *Builder) Input(key string, value any) *Builder {
	if b.flow.InitialInputs == nil {
		b.flow.InitialInputs = map[string]any{}
	}
	b.flow.InitialInputs[key] = value
	return b
}

// Add appends a node (any of the pkg/node constructors).
func (b *Builder) Add(n node.AnyNode) *Builder {
	b.flow.Nodes = append(b.flow.Nodes, n)
	return b
}

// Edge connects source -> target on the success path.
func (b *Builder) Edge(source, target string) *Builder {
	return b.edge(source, target, edge.TypeSuccess)
}

func (b *Builder) edge(source, target string, edgeType edge.Type) *Builder {
	b.flow.Edges = append(b.flow.Edges, edge.Edge{
		ID:     source + "->" + target,
		Source: source,
		Target: target,
		Type:   edgeType,
	})
	return b
}

// Build returns the assembled flow.
func (b *Builder) Build() Flow {
	return b.flow
}
