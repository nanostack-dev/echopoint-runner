package node

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const (
	defaultEmailWaitTimeoutMs = 60000
	// maxEmailWaitTimeoutMs is the lifetime of a run inbox address: a longer
	// wait would outlive the address that receives its mail.
	maxEmailWaitTimeoutMs          = 120 * 60 * 1000
	emailWaitPollInterval          = 250 * time.Millisecond
	inboxMessagesURLInputKey       = "inbox.messages_url"
	defaultEmailCodePattern        = `\b\d{6}\b`
	emailWaitFailedCode            = "EMAIL_WAIT_FAILED"
	emailWaitTimeoutCode           = "EMAIL_WAIT_TIMEOUT"
	emailWaitExpectationsFailedErr = "EMAIL_WAIT_EXPECTATIONS_FAILED"
)

// EmailWaitData configures a wait on the email the run inbox receives. With
// Expect, the wait judges a set of named events as a webhook wait does.
type EmailWaitData struct {
	TimeoutMs int `json:"timeout_ms"`
	SettleMs  int `json:"settle_ms"`
	// LinkPattern keeps the links it matches. Empty keeps every link.
	LinkPattern string `json:"link_pattern,omitempty"`
	// CodePattern finds the code in the text: capture group 1 when the
	// pattern has a group, else the whole match. Empty means six digits.
	CodePattern string               `json:"code_pattern,omitempty"`
	Expect      []WebhookExpectation `json:"expect,omitempty"`
}

// EmailWaitNode polls GET inbox.messages_url with the job token until a
// message of the run inbox passes the node's assertions, then publishes its
// parts, its links and its code. Without an assertion the first message
// matches: the run inbox receives only the mail sent to this run.
type EmailWaitNode struct {
	BaseNode

	Data EmailWaitData `json:"data"`

	patterns *emailPatterns
}

// AsEmailWaitNode safely casts an AnyNode to an EmailWaitNode.
func AsEmailWaitNode(n AnyNode) (*EmailWaitNode, bool) {
	typed, ok := n.(*EmailWaitNode)
	return typed, ok
}

func (n *EmailWaitNode) GetData() EmailWaitData {
	return n.Data
}

func (n *EmailWaitNode) InputSchema() []string {
	return []string{}
}

// OutputSchema lists the message outputs, and the outputs the node declares
// with extractors over the matched message.
func (n *EmailWaitNode) OutputSchema() []string {
	if len(n.Data.Expect) > 0 {
		return []string{outputReceived}
	}
	schema := emailOutputNames()
	for _, output := range n.GetOutputs() {
		if !slices.Contains(schema, output.Name) {
			schema = append(schema, output.Name)
		}
	}
	return schema
}

// ResolvedReferences names the {{refs}} in assertion values, which the wait
// resolves itself, as a webhook wait does.
func (n *EmailWaitNode) ResolvedReferences() []string {
	values := make([]any, 0, len(n.GetAssertions()))
	for _, assertion := range n.GetAssertions() {
		values = append(values, assertion.ExpectedValue)
	}
	for _, expectation := range n.Data.Expect {
		for _, assertion := range expectation.Assertions {
			values = append(values, assertion.ExpectedValue)
		}
	}
	return (&SchemaInference{}).ExtractTemplateVariables(values)
}

// Validate rejects a configuration the wait could never run: a timeout past
// the address lifetime, a settle window the timeout cuts short, a bad pattern
// or a malformed expected event. It compiles the patterns once.
func (n *EmailWaitNode) Validate() error {
	switch {
	case n.Data.TimeoutMs > maxEmailWaitTimeoutMs:
		return emailWaitError(fmt.Sprintf(
			"email wait timeout_ms (%d) must not exceed %d, the inbox address lifetime",
			n.Data.TimeoutMs, maxEmailWaitTimeoutMs,
		), nil)
	case len(n.Data.Expect) > 0 && n.Data.SettleMs >= n.timeoutMs():
		return emailWaitError(fmt.Sprintf(
			"email wait settle_ms (%d) must be shorter than timeout_ms (%d)", n.Data.SettleMs, n.timeoutMs(),
		), nil)
	}
	if problem := expectationProblem(n.Data.Expect); problem != "" {
		return emailWaitError(problem, nil)
	}
	patterns := emailPatterns{}
	var err error
	if n.Data.LinkPattern != "" {
		if patterns.link, err = regexp.Compile(n.Data.LinkPattern); err != nil {
			return emailWaitError("email wait link_pattern is not a valid regex", err)
		}
	}
	codePattern := n.Data.CodePattern
	if codePattern == "" {
		codePattern = defaultEmailCodePattern
	}
	if patterns.code, err = regexp.Compile(codePattern); err != nil {
		return emailWaitError("email wait code_pattern is not a valid regex", err)
	}
	n.patterns = &patterns
	return nil
}

func (n *EmailWaitNode) timeoutMs() int {
	if n.Data.TimeoutMs <= 0 {
		return defaultEmailWaitTimeoutMs
	}
	return n.Data.TimeoutMs
}

func (n *EmailWaitNode) Execute(ctx spi.ExecutionContext) (spi.AnyResult, error) {
	startTime := time.Now()
	messagesURL, token, err := n.waitInputs(ctx)
	if err != nil {
		return n.errorResult(ctx.Inputs, err, startTime), err
	}
	waitCtx, cancel := context.WithTimeout(ctx.Context(), time.Duration(n.timeoutMs())*time.Millisecond)
	defer cancel()
	client := nodeHTTPClient()
	fetch := func(fetchCtx context.Context) ([]readEmail, error) {
		messages, fetchErr := fetchInboxMessages(fetchCtx, client, messagesURL, token)
		read := make([]readEmail, len(messages))
		for i, message := range messages {
			read[i] = readMessage(message, *n.patterns)
		}
		return read, fetchErr
	}
	if len(n.Data.Expect) > 0 {
		return n.executeExpectations(waitCtx, ctx, fetch, startTime)
	}

	assertions, unresolved := resolveAssertionTemplates(ctx, n.GetAssertions())
	if len(unresolved) > 0 {
		err = unresolvedReferencesError(unresolved)
		return n.errorResult(ctx.Inputs, err, startTime), err
	}
	message, results, seen, waitErr := pollInbox(waitCtx, fetch, assertions)
	if waitErr != nil {
		failed := n.errorResult(ctx.Inputs, waitErr, startTime)
		failed.AssertionResults = results
		failed.RecentMessages = recentMessageSummaries(seen)
		failed.Closest = closestMessage(assertions, seen)
		return failed, waitErr
	}
	return n.successResult(ctx.Inputs, message, results, startTime), nil
}

func (n *EmailWaitNode) waitInputs(ctx spi.ExecutionContext) (string, string, error) {
	if n.patterns == nil {
		if err := n.Validate(); err != nil {
			return "", "", err
		}
	}
	messagesURL := lookupFlowInput(ctx, inboxMessagesURLInputKey)
	token, hasToken := spi.JobTokenFromContext(ctx.Context())
	if messagesURL == "" || !hasToken {
		return "", "", emailWaitError(
			"an email wait needs inbox.messages_url and the job token of a claimed job", nil,
		)
	}
	return messagesURL, token, nil
}

// pollInbox polls until a message passes every assertion, oldest first.
func pollInbox(
	waitCtx context.Context,
	fetch func(context.Context) ([]readEmail, error),
	assertions []CompositeAssertion,
) (readEmail, []spi.AssertionResult, []readEmail, error) {
	var last []spi.AssertionResult
	var seen []readEmail
	for {
		if err := waitCtx.Err(); err != nil {
			return readEmail{}, last, seen, emailWaitTimeout(err)
		}
		messages, fetchErr := fetch(waitCtx)
		switch {
		case fetchErr != nil && waitCtx.Err() != nil:
			return readEmail{}, last, seen, emailWaitTimeout(waitCtx.Err())
		case fetchErr != nil && webhookRequestsFetchFatal(fetchErr):
			return readEmail{}, last, seen, fetchErr
		case fetchErr == nil:
			seen = messages
			for _, message := range messages {
				results, assertErr := EvaluateAssertions(assertions, message.assertionContext())
				last = results
				if assertErr == nil {
					return message, results, nil, nil
				}
			}
		}
		if sleepErr := sleepCtx(waitCtx, emailWaitPollInterval); sleepErr != nil {
			return readEmail{}, last, seen, emailWaitTimeout(sleepErr)
		}
	}
}

func emailWaitTimeout(err error) error {
	return spi.NewUserError(emailWaitTimeoutCode, "email wait timed out without a matching message", err)
}

// closestMessage is the message that passed the most assertions of a wait
// that timed out, with each assertion's outcome.
func closestMessage(assertions []CompositeAssertion, seen []readEmail) *ClosestEvent {
	if len(assertions) == 0 {
		return nil
	}
	bestIndex, bestPassed := -1, -1
	var bestChecks []ExpectationCheck
	for i, message := range seen {
		checks, passed := checkEach(assertions, message.assertionContext())
		if passed > bestPassed {
			bestIndex, bestPassed, bestChecks = i, passed, checks
		}
	}
	if bestIndex < 0 {
		return nil
	}
	return &ClosestEvent{RequestID: seen[bestIndex].ID, Checks: bestChecks}
}

func recentMessageSummaries(seen []readEmail) []map[string]any {
	if len(seen) == 0 {
		return nil
	}
	newest := seen[max(0, len(seen)-emailRecentMessagesKept):]
	summaries := make([]map[string]any, 0, len(newest))
	for _, message := range newest {
		summaries = append(summaries, message.summary())
	}
	return summaries
}

func (n *EmailWaitNode) executeExpectations(
	waitCtx context.Context,
	ctx spi.ExecutionContext,
	fetch func(context.Context) ([]readEmail, error),
	startTime time.Time,
) (spi.AnyResult, error) {
	everyEvent, groups := resolveExpectations(ctx, n.GetAssertions(), n.Data.Expect)
	fetchEvents := func(fetchCtx context.Context) ([]waitEvent, error) {
		messages, err := fetch(fetchCtx)
		events := make([]waitEvent, len(messages))
		for i, message := range messages {
			events[i] = message
		}
		return events, err
	}
	settle := time.Duration(n.Data.SettleMs) * time.Millisecond
	outcomes, fetchErr := listenForExpectations(waitCtx, fetchEvents, groups, everyEvent, settle, startTime)
	if fetchErr == nil {
		fetchErr = expectationsFailure(emailWaitExpectationsFailedErr, outcomes)
	}
	if fetchErr != nil {
		failed := n.errorResult(ctx.Inputs, fetchErr, startTime)
		failed.Expectations = outcomes
		return failed, fetchErr
	}
	return &EmailWaitExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    spi.KindEmailWait,
			Inputs:      ctx.Inputs,
			Outputs:     map[string]any{outputReceived: countReceived(outcomes)},
			ExecutedAt:  time.Now(),
		},
		DurationMs:   time.Since(startTime).Milliseconds(),
		Expectations: outcomes,
	}, nil
}

func (n *EmailWaitNode) successResult(
	inputs map[string]any,
	message readEmail,
	assertionResults []spi.AssertionResult,
	startedAt time.Time,
) *EmailWaitExecutionResult {
	result := &EmailWaitExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:           n.GetID(),
			DisplayName:      n.GetDisplayName(),
			NodeType:         spi.KindEmailWait,
			Inputs:           inputs,
			Outputs:          message.outputs(),
			AssertionResults: assertionResults,
			ExecutedAt:       time.Now(),
		},
		DurationMs:   time.Since(startedAt).Milliseconds(),
		assertionCtx: message.assertionContext(),
	}
	log.Info().
		Str("nodeID", n.GetID()).
		Str("messageID", message.ID).
		Int64("durationMs", result.DurationMs).
		Msg("Email wait matched a message")
	return result
}

func (n *EmailWaitNode) errorResult(
	inputs map[string]any, err error, startedAt time.Time,
) *EmailWaitExecutionResult {
	errMsg := err.Error()
	errCode := spi.ErrorCode(err)
	if errCode == "" {
		errCode = emailWaitFailedCode
	}
	return &EmailWaitExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    spi.KindEmailWait,
			Inputs:      inputs,
			Error:       err,
			ErrorMsg:    &errMsg,
			ErrorCode:   &errCode,
			ExecutedAt:  time.Now(),
		},
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
}

// EmailWaitExecutionResult stores email-wait node execution data.
type EmailWaitExecutionResult struct {
	spi.BaseExecutionResult

	DurationMs int64 `json:"duration_ms"`
	// Closest is the message that passed the most assertions of a wait that
	// timed out. Its request_id holds the message id.
	Closest *ClosestEvent `json:"closest,omitempty"`
	// RecentMessages holds the newest messages a failed wait saw, at most five,
	// without their bodies.
	RecentMessages []map[string]any `json:"recent_messages,omitempty"`
	// Expectations is the verdict per expected event, in declaration order.
	Expectations []ExpectationOutcome `json:"expectations,omitempty"`

	// assertionCtx reads the matched message, so declared outputs and secret
	// outputs extract from it. It is never serialized.
	assertionCtx extractors.ResponseContext
}

// AssertionContext exposes the matched message to the engine's assertion and
// output pass. It is nil when no single message matched.
func (r *EmailWaitExecutionResult) AssertionContext() extractors.ResponseContext {
	return r.assertionCtx
}
