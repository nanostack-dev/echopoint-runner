package flowdsl

import "regexp"

var bareRefPattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_-]+)\s*\}\}`)

// lintCorrelation warns about an integration event that nothing ties to this
// run. Integration capture webhooks are shared by every run of the connection,
// so the wait takes the first delivery of the event type that passes its
// checks. A check that references a value this run produced (another node's
// output, a loop variable or a flow input) selects this run's event; a check
// on a literal or an environment value matches another run's event too.
func (g *graph) lintCorrelation() {
	ids := nodeSet(g)
	scoped := map[string]bool{}
	for _, name := range g.injected {
		scoped[name] = true
	}
	inputs := map[string]bool{}
	for _, name := range g.inputs {
		inputs[name] = true
	}
	for _, n := range g.nodes {
		if n.integ == nil || !n.integ.isEvent() {
			continue
		}
		if eventIsCorrelated(n, ids, scoped, inputs) {
			continue
		}
		g.add(
			Warning,
			n.line,
			"integration event %q has no check tied to this run: concurrent runs share the %s capture webhook, so the wait can take another run's %s event; add a check on a value this run created, for example \"$.data.object.id equals {{<node>.id}}\"",
			n.id,
			n.integ.id,
			n.integ.event,
		)
	}
}

func eventIsCorrelated(n *dslNode, ids, scoped, inputs map[string]bool) bool {
	var texts []string
	for _, item := range n.expects {
		collectStrings(item.OperatorData, &texts)
		collectStrings(item.ExtractorData, &texts)
	}
	for _, group := range n.integ.groups {
		for _, item := range group.checks {
			collectStrings(item.OperatorData, &texts)
			collectStrings(item.ExtractorData, &texts)
		}
	}
	for _, text := range texts {
		for _, match := range refPattern.FindAllStringSubmatch(text, -1) {
			if source := match[1]; (ids[source] && source != n.id) || scoped[source] {
				return true
			}
		}
		for _, match := range bareRefPattern.FindAllStringSubmatch(text, -1) {
			if inputs[match[1]] || scoped[match[1]] {
				return true
			}
		}
	}
	return false
}
