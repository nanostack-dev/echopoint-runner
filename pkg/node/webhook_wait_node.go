package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const (
	defaultWebhookWaitTimeoutMs = 30000
	webhookWaitPollInterval     = 250 * time.Millisecond
	webhookURLInputKey          = "webhook.url"
	webhookMailboxInputKey      = "webhook.mailbox_token"
)

// WebhookWaitData configures a wait on this execution's webhook mailbox.
type WebhookWaitData struct {
	TimeoutMs int `json:"timeout_ms"`
}

// WebhookWaitNode polls GET webhook.url with X-Mailbox-Token until a stored
// request passes the node's assertions. Authors never set a history URL or key.
type WebhookWaitNode struct {
	BaseNode

	Data WebhookWaitData `json:"data"`
}

// AsWebhookWaitNode safely casts an AnyNode to a WebhookWaitNode.
func AsWebhookWaitNode(n AnyNode) (*WebhookWaitNode, bool) {
	typed, ok := n.(*WebhookWaitNode)
	return typed, ok
}

func (n *WebhookWaitNode) GetData() WebhookWaitData {
	return n.Data
}

func (n *WebhookWaitNode) InputSchema() []string {
	return []string{}
}

func (n *WebhookWaitNode) OutputSchema() []string {
	return []string{"id", "method", "headers", "query", "body", "received_at"}
}

func (n *WebhookWaitNode) timeoutMs() int {
	if n.Data.TimeoutMs <= 0 {
		return defaultWebhookWaitTimeoutMs
	}
	return n.Data.TimeoutMs
}

func (n *WebhookWaitNode) Execute(ctx spi.ExecutionContext) (spi.AnyResult, error) {
	startTime := time.Now()
	mailboxURL, token, err := n.waitInputs(ctx)
	if err != nil {
		return n.errorResult(ctx.Inputs, err, startTime, nil), err
	}

	waitCtx, cancel := context.WithTimeout(ctx.Context(), time.Duration(n.timeoutMs())*time.Millisecond)
	defer cancel()

	item, results, waitErr := n.pollMailbox(waitCtx, mailboxURL, token, n.GetAssertions())
	if waitErr != nil {
		return n.errorResult(ctx.Inputs, waitErr, startTime, results), waitErr
	}
	return n.successResult(ctx.Inputs, item, results, startTime), nil
}

func (n *WebhookWaitNode) waitInputs(ctx spi.ExecutionContext) (string, string, error) {
	if len(n.GetAssertions()) == 0 {
		return "", "", spi.NewUserError(
			"WEBHOOK_WAIT_FAILED",
			"webhook wait requires at least one assertion",
			nil,
		)
	}
	mailboxURL := lookupFlowInput(ctx, webhookURLInputKey)
	token := lookupFlowInput(ctx, webhookMailboxInputKey)
	if mailboxURL == "" || token == "" {
		return "", "", spi.NewUserError(
			"WEBHOOK_WAIT_FAILED",
			"webhook.url and webhook.mailbox_token are required",
			nil,
		)
	}
	return mailboxURL, token, nil
}

func (n *WebhookWaitNode) pollMailbox(
	waitCtx context.Context,
	mailboxURL, token string,
	assertions []CompositeAssertion,
) (mailboxItem, []spi.AssertionResult, error) {
	client := &http.Client{}
	var last []spi.AssertionResult
	for {
		if err := waitCtx.Err(); err != nil {
			return mailboxItem{}, last, webhookWaitTimeout(err)
		}
		items, fetchErr := n.fetchMailbox(waitCtx, client, mailboxURL, token)
		if fetchErr != nil {
			if waitCtx.Err() != nil {
				return mailboxItem{}, last, webhookWaitTimeout(waitCtx.Err())
			}
			if mailboxFetchFatal(fetchErr) {
				return mailboxItem{}, last, fetchErr
			}
		} else if item, results, ok := firstMatchingMailboxRequest(assertions, items); ok {
			return item, results, nil
		} else {
			last = results
		}
		if sleepErr := sleepCtx(waitCtx, webhookWaitPollInterval); sleepErr != nil {
			return mailboxItem{}, last, webhookWaitTimeout(sleepErr)
		}
	}
}

func firstMatchingMailboxRequest(
	assertions []CompositeAssertion, items []mailboxItem,
) (mailboxItem, []spi.AssertionResult, bool) {
	var last []spi.AssertionResult
	for _, item := range items {
		results, assertErr := EvaluateAssertions(
			assertions, extractors.NewValueResponseContext(item.assertionValue()),
		)
		last = results
		if assertErr == nil {
			return item, results, true
		}
	}
	return mailboxItem{}, last, false
}

func webhookWaitTimeout(err error) error {
	return spi.NewUserError(
		"WEBHOOK_WAIT_TIMEOUT",
		"webhook wait timed out without a matching request",
		err,
	)
}

func (n *WebhookWaitNode) fetchMailbox(
	ctx context.Context, client *http.Client, mailboxURL, token string,
) ([]mailboxItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mailboxURL, nil)
	if err != nil {
		return nil, spi.NewUserError("WEBHOOK_WAIT_FAILED", "could not build mailbox request", err)
	}
	req.Header.Set("X-Mailbox-Token", token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mailbox get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mailbox read: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var list mailboxList
		if unmarshalErr := json.Unmarshal(raw, &list); unmarshalErr != nil {
			return nil, spi.NewUserError("WEBHOOK_WAIT_FAILED", "mailbox response is not valid JSON", unmarshalErr)
		}
		return list.Items, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, spi.NewUserError("WEBHOOK_WAIT_FAILED", "mailbox token was rejected", nil)
	case http.StatusNotFound:
		return nil, spi.NewUserError("WEBHOOK_WAIT_FAILED", "execution mailbox was not found", nil)
	default:
		return nil, fmt.Errorf("mailbox get status %d", resp.StatusCode)
	}
}

func (n *WebhookWaitNode) successResult(
	inputs map[string]any,
	item mailboxItem,
	assertionResults []spi.AssertionResult,
	startedAt time.Time,
) *WebhookWaitExecutionResult {
	result := &WebhookWaitExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:           n.GetID(),
			DisplayName:      n.GetDisplayName(),
			NodeType:         spi.KindWebhookWait,
			Inputs:           inputs,
			Outputs:          item.outputs(),
			AssertionResults: assertionResults,
			ExecutedAt:       time.Now(),
		},
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
	log.Info().
		Str("nodeID", n.GetID()).
		Str("requestID", item.ID).
		Int64("durationMs", result.DurationMs).
		Msg("Webhook wait matched a request")
	return result
}

func (n *WebhookWaitNode) errorResult(
	inputs map[string]any,
	err error,
	startedAt time.Time,
	assertionResults []spi.AssertionResult,
) spi.AnyResult {
	errMsg := err.Error()
	errCode := spi.ErrorCode(err)
	if errCode == "" {
		errCode = "WEBHOOK_WAIT_FAILED"
	}
	return &WebhookWaitExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:           n.GetID(),
			DisplayName:      n.GetDisplayName(),
			NodeType:         spi.KindWebhookWait,
			Inputs:           inputs,
			Error:            err,
			ErrorMsg:         &errMsg,
			ErrorCode:        &errCode,
			AssertionResults: assertionResults,
			ExecutedAt:       time.Now(),
		},
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
}

func lookupFlowInput(ctx spi.ExecutionContext, key string) string {
	if ctx.FlowInputs != nil {
		if value, ok := ctx.FlowInputs[key]; ok {
			if text, isText := value.(string); isText {
				return text
			}
		}
	}
	if ctx.Inputs != nil {
		if value, ok := ctx.Inputs[key]; ok {
			if text, isText := value.(string); isText {
				return text
			}
		}
	}
	return ""
}

func mailboxFetchFatal(err error) bool {
	_, ok := spi.AsUserError(err)
	return ok
}

type mailboxList struct {
	Items []mailboxItem `json:"items"`
}

type mailboxItem struct {
	ID          string            `json:"id"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	QueryParams map[string]string `json:"query_params"`
	Body        *string           `json:"body"`
	ReceivedAt  time.Time         `json:"received_at"`
}

func (item mailboxItem) assertionValue() any {
	if item.Body == nil || *item.Body == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(*item.Body), &parsed); err == nil {
		return parsed
	}
	return *item.Body
}

func (item mailboxItem) outputs() map[string]any {
	headers := item.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	query := item.QueryParams
	if query == nil {
		query = map[string]string{}
	}
	receivedAt := ""
	if !item.ReceivedAt.IsZero() {
		receivedAt = item.ReceivedAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":          item.ID,
		"method":      item.Method,
		"headers":     headers,
		"query":       query,
		"body":        item.assertionValue(),
		"received_at": receivedAt,
	}
}
