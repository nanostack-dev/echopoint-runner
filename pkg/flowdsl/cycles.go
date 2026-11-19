package flowdsl

import "strings"

// graphEdge is one outgoing edge of a node, with the field that wrote it.
type graphEdge struct {
	what   string
	target string
}

// outgoing lists a node's edges in the order the engine counts them: every
// edge type, success, branch and failure, gates the target's start.
func outgoing(n *dslNode) []graphEdge {
	edges := make([]graphEdge, 0, len(n.next)+len(n.fail))
	for _, target := range n.next {
		edges = append(edges, graphEdge{what: "next", target: target})
	}
	if n.branch != nil {
		for _, item := range n.branch.cases {
			edges = append(edges, graphEdge{what: "branch target", target: item.target})
		}
		if n.branch.hasDefault {
			edges = append(edges, graphEdge{what: "default", target: n.branch.defaultTarget})
		}
	}
	for _, target := range n.fail {
		edges = append(edges, graphEdge{what: "on_failure", target: target})
	}
	return edges
}

// cycleWalk is a depth-first walk over one graph's edges. A poll or loop body
// is its own graph, so it is walked when that body finishes resolving.
type cycleWalk struct {
	g     *graph
	byID  map[string]*dslNode
	state map[string]int
	stack []string
}

// lintCycles refuses an edge that leads back to a node on its own path. The
// engine starts a node only after all its incoming edges finish, so a cycle
// never runs: the run fails with "cycle detected or unreachable nodes".
func (g *graph) lintCycles() {
	walk := &cycleWalk{g: g, byID: map[string]*dslNode{}, state: map[string]int{}}
	for _, n := range g.nodes {
		if _, seen := walk.byID[n.id]; !seen {
			walk.byID[n.id] = n
		}
	}
	for _, n := range g.nodes {
		if walk.state[n.id] == stateFresh {
			walk.visit(n)
		}
	}
}

func (w *cycleWalk) visit(n *dslNode) {
	w.state[n.id] = stateVisit
	w.stack = append(w.stack, n.id)
	for _, edge := range outgoing(n) {
		next, known := w.byID[edge.target]
		if !known {
			continue // lintEdges reports a missing target.
		}
		switch w.state[next.id] {
		case stateFresh:
			w.visit(next)
		case stateVisit:
			w.report(n, edge)
		}
	}
	w.stack = w.stack[:len(w.stack)-1]
	w.state[n.id] = stateDone
}

func (w *cycleWalk) report(n *dslNode, edge graphEdge) {
	start := 0
	for i, id := range w.stack {
		if id == edge.target {
			start = i
			break
		}
	}
	path := append(append([]string{}, w.stack[start:]...), edge.target)
	w.g.add(Error, n.line, "node %q %s %q makes a cycle: %s", n.id, edge.what, edge.target, strings.Join(path, " -> "))
}
