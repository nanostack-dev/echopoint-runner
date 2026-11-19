package flowdsl

import (
	"regexp"
	"strings"
)

// tagPattern and maxTagsPerFlow mirror echopoint's canonical tag rule
// (internal/feature/flows/tags.go `normalizeTags`). The server trims and
// lowercases each tag before it matches, so `Smoke` is valid and stored as
// `smoke`. Checking here lets compile_flow_source and an ephemeral run report
// a tag that a save would refuse.
var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{0,19}$`)

const maxTagsPerFlow = 32

func (g *graph) checkTags(line int) {
	distinct := make(map[string]struct{}, len(g.tags))
	for _, raw := range g.tags {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if !tagPattern.MatchString(tag) {
			g.add(Error, line,
				"tag %q is invalid: tags must be lowercase, 1-20 characters, and match [a-z0-9][a-z0-9._:/-]*", raw)
			continue
		}
		distinct[tag] = struct{}{}
	}
	if len(distinct) > maxTagsPerFlow {
		g.add(Error, line, "a flow may have at most %d tags, got %d", maxTagsPerFlow, len(distinct))
	}
}
