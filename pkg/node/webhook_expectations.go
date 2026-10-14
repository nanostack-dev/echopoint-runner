package node

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	extractorshttp "github.com/nanostack-dev/echopoint-runner/pkg/extractors/http"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const expectationsFailedCode = "WEBHOOK_WAIT_EXPECTATIONS_FAILED"

// SelfResolvingNode resolves some {{refs}} itself at run time instead of
// declaring them as inputs. Validation still checks them.
type SelfResolvingNode interface {
	ResolvedReferences() []string
}

// WebhookExpectation is one event a webhook wait expects: a name over a group of
// assertions, and how many events may satisfy it.
type WebhookExpectation struct {
	Name       string               `json:"name"`
	Min        *int                 `json:"min,omitempty"`
	Max        *int                 `json:"max,omitempty"`
	Assertions []CompositeAssertion `json:"assertions"`
}

func (e WebhookExpectation) minimum() int {
	if e.Min == nil {
		return 1
	}
	return *e.Min
}

// ExpectationOutcome is the verdict on one expected event.
type ExpectationOutcome struct {
	Name     string `json:"name"`
	Received int    `json:"received"`
	Passed   bool   `json:"passed"`
	// ArrivedAfterMs is set when the event arrived while the wait was listening.
	ArrivedAfterMs *int64        `json:"arrived_after_ms,omitempty"`
	RequestIDs     []string      `json:"request_ids"`
	Problem        string        `json:"problem,omitempty"`
	Closest        *ClosestEvent `json:"closest,omitempty"`
}

// ClosestEvent is the request that passed the most assertions of a group that
// found no event, or the request another group claimed that still put a group
// over its max, with each assertion's outcome.
type ClosestEvent struct {
	RequestID string             `json:"request_id"`
	ClaimedBy string             `json:"claimed_by,omitempty"`
	Checks    []ExpectationCheck `json:"checks"`
}

type ExpectationCheck struct {
	Label    string `json:"label"`
	Passed   bool   `json:"passed"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
}

type resolvedExpectation struct {
	WebhookExpectation

	assertions []CompositeAssertion
	unresolved []string
}

func (n *WebhookWaitNode) executeExpectations(ctx spi.ExecutionContext) (spi.AnyResult, error) {
	startTime := time.Now()
	requestsURL, token, err := n.waitInputs(ctx)
	if err == nil {
		err = n.validateExpectations()
	}
	if err != nil {
		return n.errorResult(ctx.Inputs, err, startTime, nil), err
	}

	everyEvent, everyEventUnresolved := resolveAssertionTemplates(ctx, n.GetAssertions())
	groups := make([]resolvedExpectation, len(n.Data.Expect))
	for i, expectation := range n.Data.Expect {
		assertions, missing := resolveAssertionTemplates(ctx, expectation.Assertions)
		groups[i] = resolvedExpectation{
			WebhookExpectation: expectation,
			assertions:         assertions,
			unresolved:         append(missing, everyEventUnresolved...),
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx.Context(), time.Duration(n.timeoutMs())*time.Millisecond)
	defer cancel()

	outcomes, fetchErr := n.listenForExpectations(waitCtx, requestsURL, token, groups, everyEvent, startTime)
	if fetchErr != nil {
		failed := n.errorResult(ctx.Inputs, fetchErr, startTime, nil)
		failed.Expectations = outcomes
		return failed, fetchErr
	}

	failedCount := 0
	for _, outcome := range outcomes {
		if !outcome.Passed {
			failedCount++
		}
	}
	if failedCount > 0 {
		failure := spi.NewUserError(
			expectationsFailedCode,
			fmt.Sprintf("%d of %d expected events failed", failedCount, len(outcomes)),
			nil,
		)
		failed := n.errorResult(ctx.Inputs, failure, startTime, nil)
		failed.Expectations = outcomes
		return failed, failure
	}

	result := &WebhookWaitExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    spi.KindWebhookWait,
			Inputs:      ctx.Inputs,
			Outputs:     map[string]any{"received": countReceived(outcomes)},
			ExecutedAt:  time.Now(),
		},
		DurationMs:   time.Since(startTime).Milliseconds(),
		Expectations: outcomes,
	}
	return result, nil
}

func (n *WebhookWaitNode) validateExpectations() error {
	if err := n.validateSettle(); err != nil {
		return err
	}
	for i, expectation := range n.Data.Expect {
		switch {
		case strings.TrimSpace(expectation.Name) == "":
			return expectationConfigError(fmt.Sprintf("expected event %d has no name", i+1))
		case len(expectation.Assertions) == 0:
			return expectationConfigError(fmt.Sprintf("expected event %q has no assertion", expectation.Name))
		case expectation.minimum() < 0:
			return expectationConfigError(fmt.Sprintf("expected event %q has a negative min", expectation.Name))
		case expectation.Max != nil && *expectation.Max < expectation.minimum():
			return expectationConfigError(fmt.Sprintf("expected event %q has max below min", expectation.Name))
		}
	}
	return nil
}

// validateSettle rejects a settle window the timeout would always cut short.
func (n *WebhookWaitNode) validateSettle() error {
	if len(n.Data.Expect) == 0 || n.Data.SettleMs < n.timeoutMs() {
		return nil
	}
	return expectationConfigError(fmt.Sprintf(
		"webhook wait settle_ms (%d) must be shorter than timeout_ms (%d)", n.Data.SettleMs, n.timeoutMs(),
	))
}

func expectationConfigError(message string) error {
	return spi.NewUserError("WEBHOOK_WAIT_FAILED", message, nil)
}

// listenForExpectations polls until every group has its events and the settle
// window has passed, a group can no longer pass, or the wait times out. When
// no group needs an event and there is no settle window, it listens until the
// timeout. A timeout is not an error here: the outcomes say which groups went
// unmet, and a timeout inside the settle window fails the groups that passed.
func (n *WebhookWaitNode) listenForExpectations(
	waitCtx context.Context,
	requestsURL, token string,
	groups []resolvedExpectation,
	everyEvent []CompositeAssertion,
	startTime time.Time,
) ([]ExpectationOutcome, error) {
	client := nodeHTTPClient()
	listener := &expectationListener{
		groups:     groups,
		everyEvent: everyEvent,
		settle:     time.Duration(n.Data.SettleMs) * time.Millisecond,
		startTime:  startTime,
		firstSeen:  map[string]time.Duration{},
	}
	for waitCtx.Err() == nil {
		items, fetchErr := n.fetchWebhookRequests(waitCtx, client, requestsURL, token)
		if fetchErr == nil && listener.observe(items) {
			return listener.outcomes, nil
		}
		if fetchErr != nil && waitCtx.Err() == nil && webhookRequestsFetchFatal(fetchErr) {
			return listener.finalOutcomes(), fetchErr
		}
		_ = sleepCtx(waitCtx, listener.nextPollIn())
	}
	return listener.timedOutOutcomes(), nil
}

type expectationListener struct {
	groups     []resolvedExpectation
	everyEvent []CompositeAssertion
	settle     time.Duration
	startTime  time.Time
	firstSeen  map[string]time.Duration
	polledOnce bool
	settledAt  time.Time
	outcomes   []ExpectationOutcome
}

// observe judges one poll and reports whether the wait is over.
func (l *expectationListener) observe(items []capturedRequest) bool {
	elapsed := time.Since(l.startTime)
	for _, item := range items {
		if _, known := l.firstSeen[item.ID]; known {
			continue
		}
		l.firstSeen[item.ID] = 0
		if l.polledOnce {
			l.firstSeen[item.ID] = elapsed
		}
	}
	l.polledOnce = true

	var verdict expectationVerdict
	l.outcomes, verdict = evaluateExpectations(l.groups, l.everyEvent, items, l.firstSeen)
	switch verdict {
	case verdictBroken:
		return true
	case verdictMet:
		if l.settle == 0 && !l.requiresAnEvent() {
			return false
		}
		if l.settledAt.IsZero() {
			l.settledAt = time.Now().Add(l.settle)
		}
		return !time.Now().Before(l.settledAt)
	case verdictPending:
		return false
	}
	return false
}

// requiresAnEvent reports whether some group needs an event to pass. When none
// does, the wait is met on its first poll, so without a settle window it
// watches until the timeout for an event that must never arrive.
func (l *expectationListener) requiresAnEvent() bool {
	for _, group := range l.groups {
		if group.minimum() > 0 {
			return true
		}
	}
	return false
}

// nextPollIn polls again when the settle window ends rather than up to one
// interval later, so a timeout just after the window does not cut it short.
func (l *expectationListener) nextPollIn() time.Duration {
	if l.settledAt.IsZero() {
		return webhookWaitPollInterval
	}
	return max(0, min(webhookWaitPollInterval, time.Until(l.settledAt)))
}

// timedOutOutcomes fails every passing group when the timeout fired inside the
// settle window: an event it did not wait for could still have broken them.
func (l *expectationListener) timedOutOutcomes() []ExpectationOutcome {
	outcomes := l.finalOutcomes()
	if l.settledAt.IsZero() {
		return outcomes
	}
	for i := range outcomes {
		if outcomes[i].Passed {
			outcomes[i].Passed = false
			outcomes[i].Problem = fmt.Sprintf("timed out before the %dms settle window ended", l.settle.Milliseconds())
		}
	}
	return outcomes
}

// finalOutcomes covers a wait that ended before its first successful poll.
func (l *expectationListener) finalOutcomes() []ExpectationOutcome {
	if l.outcomes == nil {
		l.outcomes, _ = evaluateExpectations(l.groups, nil, nil, nil)
	}
	return l.outcomes
}

type expectationVerdict int

const (
	verdictPending expectationVerdict = iota
	verdictMet
	verdictBroken
)

// evaluateExpectations judges every group against the requests received so far.
func evaluateExpectations(
	groups []resolvedExpectation,
	everyEvent []CompositeAssertion,
	items []capturedRequest,
	firstSeen map[string]time.Duration,
) ([]ExpectationOutcome, expectationVerdict) {
	contexts := make([]extractors.ResponseContext, len(items))
	for itemIndex, item := range items {
		contexts[itemIndex] = item.assertionContext()
	}
	claims, matches, claimedBy := assignRequests(groups, contexts)

	verdict := verdictMet
	outcomes := make([]ExpectationOutcome, len(groups))
	for groupIndex, group := range groups {
		events := groupEvents{claimed: claims[groupIndex], matched: matches[groupIndex], foreign: -1}
		for _, itemIndex := range events.matched {
			if owner := claimedBy[itemIndex]; owner != groupIndex {
				events.foreign, events.foreignOwner = itemIndex, groups[owner].Name
				break
			}
		}
		outcome, broken := judgeExpectation(group, events, items, contexts, everyEvent, firstSeen)
		evaluated := len(group.unresolved) == 0
		switch {
		case events.overMax(group) && events.foreign >= 0:
			checks, _ := checkEach(group.assertions, contexts[events.foreign])
			outcome.Closest = &ClosestEvent{
				RequestID: items[events.foreign].ID, ClaimedBy: events.foreignOwner, Checks: checks,
			}
		case evaluated && !outcome.Passed && outcome.Received < group.minimum():
			outcome.Closest = closestEvent(group, items, contexts, claimedBy, groups)
		}
		outcomes[groupIndex] = outcome
		if broken {
			verdict = verdictBroken
		} else if evaluated && !outcome.Passed && verdict == verdictMet {
			verdict = verdictPending
		}
	}
	return outcomes, verdict
}

// groupEvents are the requests one group claimed, which count toward its min,
// and every request that passes its assertions, which counts against its max.
// foreign is the first match another group claimed, or -1.
type groupEvents struct {
	claimed      []int
	matched      []int
	foreign      int
	foreignOwner string
}

func (e groupEvents) overMax(group resolvedExpectation) bool {
	return len(group.unresolved) == 0 && group.Max != nil && len(e.matched) > *group.Max
}

// assignRequests gives each request, oldest first, to one group: the first
// group it satisfies that still needs events, else the first group it
// satisfies, where it counts as an extra. It also lists, per group, every
// request that satisfies it, claimed or not.
func assignRequests(
	groups []resolvedExpectation, contexts []extractors.ResponseContext,
) ([][]int, [][]int, map[int]int) {
	claims := make([][]int, len(groups))
	matches := make([][]int, len(groups))
	claimedBy := make(map[int]int, len(contexts))
	for itemIndex, rc := range contexts {
		chosen, needed := -1, false
		for groupIndex, group := range groups {
			if len(group.unresolved) > 0 || !allAssertionsPass(group.assertions, rc) {
				continue
			}
			matches[groupIndex] = append(matches[groupIndex], itemIndex)
			switch {
			case needed:
			case len(claims[groupIndex]) < group.minimum():
				chosen, needed = groupIndex, true
			case chosen < 0:
				chosen = groupIndex
			}
		}
		if chosen >= 0 {
			claims[chosen] = append(claims[chosen], itemIndex)
			claimedBy[itemIndex] = chosen
		}
	}
	return claims, matches, claimedBy
}

// judgeExpectation reports the group's outcome, and whether it failed in a way
// no later request can repair: too many events, or a claimed event that fails a
// check every event must pass.
func judgeExpectation(
	group resolvedExpectation,
	events groupEvents,
	items []capturedRequest,
	contexts []extractors.ResponseContext,
	everyEvent []CompositeAssertion,
	firstSeen map[string]time.Duration,
) (ExpectationOutcome, bool) {
	claimed := events.claimed
	outcome := ExpectationOutcome{Name: group.Name, Received: len(claimed), RequestIDs: []string{}}
	for _, itemIndex := range claimed {
		outcome.RequestIDs = append(outcome.RequestIDs, items[itemIndex].ID)
	}
	if len(claimed) > 0 {
		if arrived := firstSeen[items[claimed[0]].ID]; arrived > 0 {
			ms := arrived.Milliseconds()
			outcome.ArrivedAfterMs = &ms
		}
	}

	switch {
	case len(group.unresolved) > 0:
		outcome.Problem = "not evaluated: no value for {{" + group.unresolved[0] + "}}"
		return outcome, false
	case events.overMax(group):
		outcome.Problem = fmt.Sprintf("%d arrived", len(events.matched))
		if events.foreign >= 0 {
			outcome.Problem += fmt.Sprintf("; %s counts against max %d although %q claimed it",
				items[events.foreign].ID, *group.Max, events.foreignOwner)
		}
		return outcome, true
	}
	for _, itemIndex := range claimed {
		if failed := firstFailingAssertion(everyEvent, contexts[itemIndex]); failed != nil {
			outcome.Problem = describeAssertion(*failed) + " failed on " + items[itemIndex].ID
			return outcome, true
		}
	}
	switch {
	case len(claimed) == 0 && group.minimum() > 0:
		outcome.Problem = "missing"
	case len(claimed) < group.minimum():
		outcome.Problem = fmt.Sprintf("%d of %d", len(claimed), group.minimum())
	default:
		outcome.Passed = true
	}
	return outcome, false
}

func closestEvent(
	group resolvedExpectation,
	items []capturedRequest,
	contexts []extractors.ResponseContext,
	claimedBy map[int]int,
	groups []resolvedExpectation,
) *ClosestEvent {
	bestIndex, bestPassed := -1, 0
	var bestChecks []ExpectationCheck
	for itemIndex := range items {
		checks, passed := checkEach(group.assertions, contexts[itemIndex])
		if passed > bestPassed {
			bestIndex, bestPassed, bestChecks = itemIndex, passed, checks
		}
	}
	if bestIndex < 0 {
		return nil
	}
	closest := &ClosestEvent{RequestID: items[bestIndex].ID, Checks: bestChecks}
	if owner, claimed := claimedBy[bestIndex]; claimed {
		closest.ClaimedBy = groups[owner].Name
	}
	return closest
}

func checkEach(assertions []CompositeAssertion, rc extractors.ResponseContext) ([]ExpectationCheck, int) {
	checks := make([]ExpectationCheck, len(assertions))
	passed := 0
	for i := range assertions {
		result := assertions[i].Evaluate(rc)
		checks[i] = ExpectationCheck{Label: describeAssertion(assertions[i]), Passed: result.Passed}
		if result.Passed {
			passed++
		} else {
			checks[i].Expected = result.Expected
			checks[i].Actual = result.Actual
		}
	}
	return checks, passed
}

func allAssertionsPass(assertions []CompositeAssertion, rc extractors.ResponseContext) bool {
	return firstFailingAssertion(assertions, rc) == nil
}

func firstFailingAssertion(assertions []CompositeAssertion, rc extractors.ResponseContext) *CompositeAssertion {
	for i := range assertions {
		if !assertions[i].Evaluate(rc).Passed {
			return &assertions[i]
		}
	}
	return nil
}

func countReceived(outcomes []ExpectationOutcome) int {
	total := 0
	for _, outcome := range outcomes {
		total += outcome.Received
	}
	return total
}

// describeAssertion renders an assertion the way the run report names it:
// what it reads, the operator, and the resolved expected value.
func describeAssertion(assertion CompositeAssertion) string {
	target := assertion.ExtractorType
	switch extractor := assertion.Extractor.(type) {
	case extractors.JSONPathExtractor:
		target = extractor.Path
	case extractors.XMLPathExtractor:
		target = extractor.Path
	case extractorshttp.HeaderExtractor:
		target = extractor.HeaderName
	case extractorshttp.QueryParamExtractor:
		target = "?" + extractor.ParamName
	}
	parts := []string{target, string(assertion.OperatorType)}
	if assertion.ExpectedValue != nil {
		parts = append(parts, fmt.Sprintf("%v", assertion.ExpectedValue))
	}
	return strings.Join(parts, " ")
}

// resolveAssertionTemplates resolves {{refs}} in expected values against the
// outputs of completed steps and the flow inputs. It returns the refs that had
// no value; an assertion holding one keeps its template text.
func resolveAssertionTemplates(
	ctx spi.ExecutionContext, assertions []CompositeAssertion,
) ([]CompositeAssertion, []string) {
	resolved := make([]CompositeAssertion, len(assertions))
	var unresolved []string
	for i, assertion := range assertions {
		resolved[i] = assertion
		refs := (&SchemaInference{}).ExtractTemplateVariables(assertion.ExpectedValue)
		if len(refs) == 0 && !containsDynamicTemplate(assertion.ExpectedValue) {
			continue
		}
		values := make(map[string]any, len(refs))
		for _, ref := range refs {
			value, found := lookupReference(ctx, ref)
			if !found {
				unresolved = append(unresolved, ref)
				continue
			}
			values[ref] = value
		}
		expected, err := NewTemplateResolverWithDynamics(values, ctx.DynamicVars).Resolve(assertion.ExpectedValue)
		if err == nil {
			resolved[i].ExpectedValue = expected
		}
	}
	return resolved, unresolved
}

func containsDynamicTemplate(value any) bool {
	text, isText := value.(string)
	return isText && strings.Contains(text, "{{$")
}

// lookupReference reads "node.output" from a completed step, else the whole
// reference as a flow input, such as {{webhook.url}} or {{apiUrl}}. Like any
// template, a dotted path walks into the value: {{producer.account.id}} or a
// loop's {{account.id}}.
func lookupReference(ctx spi.ExecutionContext, ref string) (any, bool) {
	// The namespace is chosen once: a reference rooted at a completed step only
	// reads that step's outputs, and never falls back to a flow input.
	if nodeID, _, dotted := strings.Cut(ref, "."); dotted && ctx.AllOutputs != nil &&
		ctx.AllOutputs.HasNode(nodeID) {
		return LookupPath(ref, func(key string) (any, bool) {
			keyNode, outputKey, keyDotted := strings.Cut(key, ".")
			if !keyDotted || keyNode != nodeID {
				return nil, false
			}
			return ctx.AllOutputs.Get(nodeID, outputKey)
		})
	}
	return LookupPath(ref, func(key string) (any, bool) {
		value, found := ctx.FlowInputs[key]
		return value, found
	})
}

func unresolvedReferencesError(refs []string) error {
	return spi.NewUserError(
		"ASSERTION_FAILED",
		"assertion references {{"+refs[0]+"}}, which has no value",
		nil,
	)
}
