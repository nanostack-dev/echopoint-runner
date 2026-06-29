package node

// Fluent constructors for building nodes in Go (tests, the CLI, embedded users)
// instead of hand-writing JSON. Each returns the concrete node, which satisfies
// AnyNode, so they compose with flow.Builder.

// NewRequest starts a request node with the given id.
func NewRequest(id string) *RequestNode {
	return &RequestNode{BaseNode: BaseNode{ID: id, NodeType: TypeRequest}}
}

// DisplayName sets the human-readable name.
func (n *RequestNode) DisplayName(name string) *RequestNode {
	n.BaseNode.DisplayName = name
	return n
}

// Method sets the HTTP method + URL.
func (n *RequestNode) Method(method, url string) *RequestNode {
	n.Data.Method = method
	n.Data.URL = url
	return n
}

// GET is a shorthand for Method("GET", url).
func (n *RequestNode) GET(url string) *RequestNode { return n.Method("GET", url) }

// POST is a shorthand for Method("POST", url).
func (n *RequestNode) POST(url string) *RequestNode { return n.Method("POST", url) }

// PUT is a shorthand for Method("PUT", url).
func (n *RequestNode) PUT(url string) *RequestNode { return n.Method("PUT", url) }

// PATCH is a shorthand for Method("PATCH", url).
func (n *RequestNode) PATCH(url string) *RequestNode { return n.Method("PATCH", url) }

// DELETE is a shorthand for Method("DELETE", url).
func (n *RequestNode) DELETE(url string) *RequestNode { return n.Method("DELETE", url) }

// Header adds a request header (supports {{var}} templates).
func (n *RequestNode) Header(key, value string) *RequestNode {
	if n.Data.Headers == nil {
		n.Data.Headers = map[string]string{}
	}
	n.Data.Headers[key] = value
	return n
}

// Body sets the request body (any JSON-serializable value, with {{var}} support).
func (n *RequestNode) Body(body any) *RequestNode {
	n.Data.Body = body
	return n
}

// TimeoutMs sets the per-request timeout in milliseconds.
func (n *RequestNode) TimeoutMs(ms int) *RequestNode {
	n.Data.Timeout = ms
	return n
}

// Assert appends a validation assertion.
func (n *RequestNode) Assert(assertion CompositeAssertion) *RequestNode {
	n.BaseNode.Assertions = append(n.BaseNode.Assertions, assertion)
	return n
}

// Output appends an extracted output (referenced downstream as "id.name").
func (n *RequestNode) Output(output Output) *RequestNode {
	n.BaseNode.Outputs = append(n.BaseNode.Outputs, output)
	return n
}

// Always marks the node to run in the cleanup phase even after a main-phase failure.
func (n *RequestNode) Always() *RequestNode {
	n.BaseNode.RunWhen = RunWhenAlways
	return n
}

// NewDelay starts a delay node that waits durationMs milliseconds.
func NewDelay(id string, durationMs int) *DelayNode {
	return &DelayNode{
		BaseNode: BaseNode{ID: id, NodeType: TypeDelay},
		Data:     DelayData{Duration: durationMs},
	}
}

// DisplayName sets the delay node's name.
func (n *DelayNode) DisplayName(name string) *DelayNode {
	n.BaseNode.DisplayName = name
	return n
}

// Always marks the delay to run in the cleanup phase.
func (n *DelayNode) Always() *DelayNode {
	n.BaseNode.RunWhen = RunWhenAlways
	return n
}

// DisplayName sets the module node's name.
func (n *ModuleNode) DisplayName(name string) *ModuleNode {
	n.BaseNode.DisplayName = name
	return n
}

// InputBinding binds a child-flow input to a value/template.
func (n *ModuleNode) InputBinding(key string, value any) *ModuleNode {
	if n.Data.InputBindings == nil {
		n.Data.InputBindings = map[string]any{}
	}
	n.Data.InputBindings[key] = value
	return n
}

// OutputBinding maps a child final output key to a parent-visible output name.
func (n *ModuleNode) OutputBinding(parentName, childKey string) *ModuleNode {
	if n.Data.OutputBindings == nil {
		n.Data.OutputBindings = map[string]string{}
	}
	n.Data.OutputBindings[parentName] = childKey
	return n
}
