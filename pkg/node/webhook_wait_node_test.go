package node_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func mkWebhookWaitJSON(t *testing.T, timeoutMs int, withAssertion bool) []byte {
	t.Helper()
	assertions := "[]"
	if withAssertion {
		assertions = `[{"extractor_type":"jsonPath","extractor_data":{"path":"$.event"},` +
			`"operator_type":"equals","operator_data":{"value":"order.created"}}]`
	}
	return fmt.Appendf(nil,
		`{"id":"wait-1","display_name":"Wait for webhook","type":"webhook_wait",`+
			`"assertions":%s,"data":{"timeout_ms":%d}}`,
		assertions, timeoutMs,
	)
}

func decodeWebhookWait(t *testing.T, raw []byte) *node.WebhookWaitNode {
	t.Helper()
	n, err := node.UnmarshalNode(raw)
	if err != nil {
		t.Fatalf("UnmarshalNode: %v", err)
	}
	wait, ok := node.AsWebhookWaitNode(n)
	if !ok {
		t.Fatalf("expected *WebhookWaitNode, got %T", n)
	}
	return wait
}

func mailboxJSON(items ...map[string]any) []byte {
	raw, err := json.Marshal(map[string]any{"items": items, "count": len(items), "total": len(items)})
	if err != nil {
		panic(err)
	}
	return raw
}

func mailboxItem(id, body string) map[string]any {
	return map[string]any{
		"id":           id,
		"method":       http.MethodPost,
		"headers":      map[string]string{"content-type": "application/json"},
		"query_params": map[string]string{},
		"body":         body,
		"received_at":  time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
}

func TestWebhookWaitNode_DecodeDefaults(t *testing.T) {
	n := decodeWebhookWait(t, mkWebhookWaitJSON(t, 0, true))
	if n.GetType() != spi.KindWebhookWait {
		t.Errorf("type=%s", n.GetType())
	}
	if n.GetRunWhen() != spi.RunWhenOnSuccess {
		t.Errorf("run_when=%s", n.GetRunWhen())
	}
	if got := node.WebhookWaitTimeoutMsForTest(n); got != 30000 {
		t.Errorf("default timeout_ms=%d", got)
	}
}

func TestWebhookWaitNode_RequiresAssertion(t *testing.T) {
	n := decodeWebhookWait(t, mkWebhookWaitJSON(t, 50, false))
	_, err := n.Execute(spi.ExecutionContext{
		FlowInputs: map[string]any{
			"webhook.url":           "http://example.invalid/mailbox",
			"webhook.mailbox_token": "tok",
		},
	})
	if err == nil {
		t.Fatal("expected assertion guard")
	}
	if spi.ErrorCode(err) != "WEBHOOK_WAIT_FAILED" {
		t.Errorf("code=%s", spi.ErrorCode(err))
	}
}

func TestWebhookWaitNode_SucceedsOnFirstMatchingRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method=%s", r.Method)
		}
		if r.Header.Get("X-Mailbox-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			_, _ = w.Write(mailboxJSON())
		case 2:
			_, _ = w.Write(mailboxJSON(mailboxItem("req-1", `{"event":"other"}`)))
		default:
			_, _ = w.Write(mailboxJSON(
				mailboxItem("req-1", `{"event":"other"}`),
				mailboxItem("req-2", `{"event":"order.created"}`),
			))
		}
	}))
	defer srv.Close()

	n := decodeWebhookWait(t, mkWebhookWaitJSON(t, 2000, true))
	res, err := n.Execute(spi.ExecutionContext{
		FlowInputs: map[string]any{
			"webhook.url":           srv.URL,
			"webhook.mailbox_token": "tok",
		},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	waitRes, ok := spi.As[*node.WebhookWaitExecutionResult](res)
	if !ok {
		t.Fatalf("got %T", res)
	}
	if waitRes.Outputs["id"] != "req-2" {
		t.Errorf("id=%v", waitRes.Outputs["id"])
	}
	if waitRes.Outputs["method"] != http.MethodPost {
		t.Errorf("method=%v", waitRes.Outputs["method"])
	}
	body, _ := waitRes.Outputs["body"].(map[string]any)
	if body["event"] != "order.created" {
		t.Errorf("body=%v", waitRes.Outputs["body"])
	}
	if calls.Load() < 3 {
		t.Errorf("calls=%d", calls.Load())
	}
}

func TestWebhookWaitNode_RejectedTokenIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	n := decodeWebhookWait(t, mkWebhookWaitJSON(t, 2000, true))
	_, err := n.Execute(spi.ExecutionContext{
		FlowInputs: map[string]any{
			"webhook.url":           srv.URL,
			"webhook.mailbox_token": "bad",
		},
	})
	if err == nil {
		t.Fatal("expected fatal token error")
	}
	if spi.ErrorCode(err) != "WEBHOOK_WAIT_FAILED" {
		t.Errorf("code=%s", spi.ErrorCode(err))
	}
}

func TestWebhookWaitNode_TimeoutWithoutMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mailboxJSON(mailboxItem("req-1", `{"event":"other"}`)))
	}))
	defer srv.Close()

	n := decodeWebhookWait(t, mkWebhookWaitJSON(t, 400, true))
	_, err := n.Execute(spi.ExecutionContext{
		FlowInputs: map[string]any{
			"webhook.url":           srv.URL,
			"webhook.mailbox_token": "tok",
		},
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if spi.ErrorCode(err) != "WEBHOOK_WAIT_TIMEOUT" {
		t.Errorf("code=%s", spi.ErrorCode(err))
	}
}
