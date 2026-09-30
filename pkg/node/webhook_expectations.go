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
// found no event, with each assertion's outcome.
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

func expectationConfigError(message string) error {
	return spi.NewUserError("WEBHOOK_WAIT_FAILED", message, nil)
}

// listenForExpectations polls until every group has its events and the settle
// window has passed, a group can no longer pass, or the wait times out. A
// timeout is not an error here: the outcomes say which groups went unmet.
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
		_ = sleepCtx(waitCtx, webhookWaitPollInterval)
	}
	return listener.finalOutcomes(), nil
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
		if l.settledAt.IsZero() {
			l.settledAt = time.Now().Add(l.settle)
		}
		return !time.Now().Before(l.settledAt)
	case verdictPending:
		return false
	}
	return false
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
	claims, claimedBy := assignRequests(groups, contexts)

	verdict := verdictMet
	outcomes := make([]ExpectationOutcome, len(groups))
	for groupIndex, group := range groups {
		outcome, broken := judgeExpectation(group, claims[groupIndex], items, contexts, everyEvent, firstSeen)
		evaluated := len(group.unresolved) == 0
		if evaluated && !outcome.Passed && outcome.Received < group.minimum() {
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

// assignRequests gives each request, oldest first, to one group: the first
// group it satisfies that still needs events, else the first group it
// satisfies, where it counts as an extra.
func assignRequests(
	groups []resolvedExpectation, contexts []extractors.ResponseContext,
) ([][]int, map[int]int) {
	claims := make([][]int, len(groups))
	claimedBy := make(map[int]int, len(contexts))
	for itemIndex, rc := range contexts {
		chosen := -1
		for groupIndex, group := range groups {
			if len(group.unresolved) > 0 || !allAssertionsPass(group.assertions, rc) {
				continue
			}
			if len(claims[groupIndex]) < group.minimum() {
				chosen = groupIndex
				break
			}
			if chosen < 0 {
				chosen = groupIndex
			}
		}
		if chosen >= 0 {
			claims[chosen] = append(claims[chosen], itemIndex)
			claimedBy[itemIndex] = chosen
		}
	}
	return claims, claimedBy
}

// judgeExpectation reports the group's outcome, and whether it failed in a way
// no later request can repair: too many events, or a claimed event that fails a
// check every event must pass.
func judgeExpectation(
	group resolvedExpectation,
	claimed []int,
	items []capturedRequest,
	contexts []extractors.ResponseContext,
	everyEvent []CompositeAssertion,
	firstSeen map[string]time.Duration,
) (ExpectationOutcome, bool) {
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
	case group.Max != nil && len(claimed) > *group.Max:
		outcome.Problem = fmt.Sprintf("%d arrived", len(claimed))
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
// reference as a flow input, such as {{webhook.url}} or {{apiUrl}}.
func lookupReference(ctx spi.ExecutionContext, ref string) (any, bool) {
	if nodeID, outputKey, dotted := strings.Cut(ref, "."); dotted && ctx.AllOutputs != nil &&
		ctx.AllOutputs.HasNode(nodeID) {
		return ctx.AllOutputs.Get(nodeID, outputKey)
	}
	value, found := ctx.FlowInputs[ref]
	return value, found
}

func unresolvedReferencesError(refs []string) error {
	return spi.NewUserError(
		"ASSERTION_FAILED",
		"webhook wait assertion references {{"+refs[0]+"}}, which has no value",
		nil,
	)
}
