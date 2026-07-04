package nodes

import (
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/assert"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
)

// requireRefs verifies every assertion's path root (the first segment — a node
// id or a flow-input name) exists in the input context. It turns a reference to
// an unexecuted or misspelled node into a clear UNKNOWN_REFERENCE error rather
// than a silent "path not found" assertion failure. Used by assert/branch, which
// address arbitrary already-executed nodes via the store.
func requireRefs(in value.Value, specs []assert.Spec) error {
	for _, s := range specs {
		root := refRoot(s.Path)
		if root == "" {
			continue
		}
		if _, ok := in.Get(root); !ok {
			return node.UserErrf("UNKNOWN_REFERENCE", "references unknown node or input %q", root)
		}
	}
	return nil
}

// refRoot returns the first path segment — the node id or flow-input name —
// stopping at the first "." or "[" ("login" from "$.login.token",
// "users" from "$.users[?@.role=='admin']").
func refRoot(path string) string {
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	if i := strings.IndexAny(path, ".["); i >= 0 {
		return path[:i]
	}
	return path
}
