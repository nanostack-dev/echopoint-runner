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
	webhookRequestsURLInputKey  = "webhook.requests_url"
	webhookReadInputKey         = "webhook.read_token"
)

// WebhookWaitData configures a wait on the webhook requests captured for this execution.
type WebhookWaitData struct {
	TimeoutMs int `json:"timeout_ms"`
}

// WebhookWaitNode polls GET webhook.requests_url with X-Webhook-Read-Token until a stored
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
	requestsURL, token, err := n.waitInputs(ctx)
	if err != nil {
		return n.errorResult(ctx.Inputs, err, startTime, nil), err
	}

	waitCtx, cancel := context.WithTimeout(ctx.Context(), time.Duration(n.timeoutMs())*time.Millisecond)
	defer cancel()

	item, results, waitErr := n.pollWebhookRequests(waitCtx, requestsURL, token, n.GetAssertions())
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
	requestsURL := lookupFlowInput(ctx, webhookRequestsURLInputKey)
	token := lookupFlowInput(ctx, webhookReadInputKey)
	if requestsURL == "" || token == "" {
		return "", "", spi.NewUserError(
			"WEBHOOK_WAIT_FAILED",
			"webhook.requests_url and webhook.read_token are required",
			nil,
		)
	}
	return requestsURL, token, nil
}

func (n *WebhookWaitNode) pollWebhookRequests(
	waitCtx context.Context,
	requestsURL, token string,
	assertions []CompositeAssertion,
) (capturedRequest, []spi.AssertionResult, error) {
	client := &http.Client{}
	var last []spi.AssertionResult
	for {
		if err := waitCtx.Err(); err != nil {
			return capturedRequest{}, last, webhookWaitTimeout(err)
		}
		items, fetchErr := n.fetchWebhookRequests(waitCtx, client, requestsURL, token)
		if fetchErr != nil {
			if waitCtx.Err() != nil {
				return capturedRequest{}, last, webhookWaitTimeout(waitCtx.Err())
			}
			if webhookRequestsFetchFatal(fetchErr) {
				return capturedRequest{}, last, fetchErr
			}
		} else if item, results, ok := firstMatchingWebhookRequest(assertions, items); ok {
			return item, results, nil
		} else {
			last = results
		}
		if sleepErr := sleepCtx(waitCtx, webhookWaitPollInterval); sleepErr != nil {
			return capturedRequest{}, last, webhookWaitTimeout(sleepErr)
		}
	}
}

func firstMatchingWebhookRequest(
	assertions []CompositeAssertion, items []capturedRequest,
) (capturedRequest, []spi.AssertionResult, bool) {
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
	return capturedRequest{}, last, false
}

func webhookWaitTimeout(err error) error {
	return spi.NewUserError(
		"WEBHOOK_WAIT_TIMEOUT",
		"webhook wait timed out without a matching request",
		err,
	)
}

func (n *WebhookWaitNode) fetchWebhookRequests(
	ctx context.Context, client *http.Client, requestsURL, token string,
) ([]capturedRequest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestsURL, nil)
	if err != nil {
		return nil, spi.NewUserError("WEBHOOK_WAIT_FAILED", "could not build the webhook requests request", err)
	}
	req.Header.Set("X-Webhook-Read-Token", token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webhook requests get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("webhook requests read: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var list webhookRequestList
		if unmarshalErr := json.Unmarshal(raw, &list); unmarshalErr != nil {
			return nil, spi.NewUserError(
				"WEBHOOK_WAIT_FAILED",
				"webhook requests response is not valid JSON",
				unmarshalErr,
			)
		}
		return list.Items, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, spi.NewUserError("WEBHOOK_WAIT_FAILED", "webhook read token was rejected", nil)
	case http.StatusNotFound:
		return nil, spi.NewUserError("WEBHOOK_WAIT_FAILED", "execution webhook requests were not found", nil)
	default:
		return nil, fmt.Errorf("webhook requests get status %d", resp.StatusCode)
	}
}

func (n *WebhookWaitNode) successResult(
	inputs map[string]any,
	item capturedRequest,
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
	return ""
}

func webhookRequestsFetchFatal(err error) bool {
	_, ok := spi.AsUserError(err)
	return ok
}

type webhookRequestList struct {
	Items []capturedRequest `json:"items"`
}

type capturedRequest struct {
	ID          string            `json:"id"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	QueryParams map[string]string `json:"query_params"`
	Body        *string           `json:"body"`
	ReceivedAt  time.Time         `json:"received_at"`
}

func (item capturedRequest) assertionValue() any {
	if item.Body == nil || *item.Body == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(*item.Body), &parsed); err == nil {
		return parsed
	}
	return *item.Body
}

func (item capturedRequest) outputs() map[string]any {
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
