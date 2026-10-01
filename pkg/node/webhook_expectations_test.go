package node_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type expectGroup struct {
	Name       string           `json:"name"`
	Min        *int             `json:"min,omitempty"`
	Max        *int             `json:"max,omitempty"`
	Assertions []map[string]any `json:"assertions"`
}

func bodyEquals(path string, value any) map[string]any {
	return map[string]any{
		"extractor_type": "jsonPath",
		"extractor_data": map[string]any{"path": path},
		"operator_type":  "equals",
		"operator_data":  map[string]any{"value": value},
	}
}

func headerStartsWith(name, prefix string) map[string]any {
	return map[string]any{
		"extractor_type": "header",
		"extractor_data": map[string]any{"header_name": name},
		"operator_type":  "startsWith",
		"operator_data":  map[string]any{"value": prefix},
	}
}

func invitationGroup(name, eventType, invitationRef string) expectGroup {
	return expectGroup{
		Name: name,
		Assertions: []map[string]any{
			bodyEquals("$.type", eventType),
			bodyEquals("$.data.invitation_id", invitationRef),
		},
	}
}

func decodeExpectWait(
	t *testing.T, timeoutMs, settleMs int, groups []expectGroup, everyEvent ...map[string]any,
) *node.WebhookWaitNode {
	t.Helper()
	if everyEvent == nil {
		everyEvent = []map[string]any{}
	}
	raw, err := json.Marshal(map[string]any{
		"id":         "events",
		"type":       "webhook_wait",
		"run_when":   "always",
		"assertions": everyEvent,
		"data":       map[string]any{"timeout_ms": timeoutMs, "settle_ms": settleMs, "expect": groups},
	})
	if err != nil {
		t.Fatal(err)
	}
	return decodeWebhookWait(t, raw)
}

func event(id, eventType, invitationID string) map[string]any {
	request := capturedRequest(id, `{"type":"`+eventType+`","data":{"invitation_id":"`+invitationID+`"}}`)
	request["headers"] = map[string]string{"webhook-signature": "v1,abc"}
	return request
}

// serveBatches answers each poll with the next batch, then repeats the last one.
func serveBatches(t *testing.T, batches ...[]map[string]any) string {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := min(int(calls.Add(1))-1, len(batches)-1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(webhookRequestsJSON(batches[index]...))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func runExpectWait(
	t *testing.T, wait *node.WebhookWaitNode, requestsURL string, outputs map[string]map[string]any,
) (*node.WebhookWaitExecutionResult, error) {
	t.Helper()
	res, err := wait.Execute(spi.ExecutionContext{
		Ctx:        spi.WithJobToken(context.Background(), "tok"),
		FlowInputs: map[string]any{"webhook.requests_url": requestsURL},
		AllOutputs: node.NewOutputView(outputs),
	})
	waitRes, ok := spi.As[*node.WebhookWaitExecutionResult](res)
	if !ok {
		t.Fatalf("got %T", res)
	}
	return waitRes, err
}

var inviteOutputs = map[string]map[string]any{
	"invite-a": {"id": "oinv_A"},
	"invite-b": {"id": "oinv_B"},
}

func TestWebhookWaitExpect_PassesWhenEveryGroupHasItsEvent(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.created", "oinv_A"),
		event("req-2", "organization.invitation.deleted", "oinv_B"),
	})
	wait := decodeExpectWait(t, 2000, 0, []expectGroup{
		invitationGroup("A created", "organization.invitation.created", "{{invite-a.id}}"),
		invitationGroup("B deleted", "organization.invitation.deleted", "{{invite-b.id}}"),
	})

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(res.Expectations) != 2 || !res.Expectations[0].Passed || !res.Expectations[1].Passed {
		t.Fatalf("expectations=%+v", res.Expectations)
	}
	if got := res.Expectations[1].RequestIDs; len(got) != 1 || got[0] != "req-2" {
		t.Errorf("request ids=%v", got)
	}
}

func TestWebhookWaitExpect_TemplateTiesTheGroupToItsInvitation(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.updated", "oinv_A"),
	})
	wait := decodeExpectWait(t, 400, 0, []expectGroup{
		invitationGroup("B resent", "organization.invitation.updated", "{{invite-b.id}}"),
	})

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if spi.ErrorCode(err) != "WEBHOOK_WAIT_EXPECTATIONS_FAILED" {
		t.Fatalf("code=%s err=%v", spi.ErrorCode(err), err)
	}
	if res.Expectations[0].Problem != "missing" {
		t.Errorf("problem=%q", res.Expectations[0].Problem)
	}
}

func TestWebhookWaitExpect_OneEventCountsForOneGroup(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.updated", "oinv_A"),
	})
	wait := decodeExpectWait(t, 400, 0, []expectGroup{
		invitationGroup("Role changed", "organization.invitation.updated", "{{invite-a.id}}"),
		invitationGroup("Resent", "organization.invitation.updated", "{{invite-a.id}}"),
	})

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if err == nil {
		t.Fatal("expected the second group to miss")
	}
	if !res.Expectations[0].Passed || res.Expectations[1].Passed {
		t.Fatalf("expectations=%+v", res.Expectations)
	}
	closest := res.Expectations[1].Closest
	if closest == nil || closest.RequestID != "req-1" || closest.ClaimedBy != "Role changed" {
		t.Errorf("closest=%+v", closest)
	}
}

func TestWebhookWaitExpect_ClosestEventNamesTheFailingCheck(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.updated", "oinv_A"),
		event("req-2", "organization.membership.created", "oinv_B"),
	})
	wait := decodeExpectWait(t, 400, 0, []expectGroup{
		invitationGroup("B resent", "organization.invitation.updated", "{{invite-b.id}}"),
	})

	res, _ := runExpectWait(t, wait, url, inviteOutputs)
	closest := res.Expectations[0].Closest
	if closest == nil || closest.RequestID != "req-1" {
		t.Fatalf("closest=%+v", closest)
	}
	failed := closest.Checks[1]
	if failed.Passed || failed.Label != "$.data.invitation_id equals oinv_B" || failed.Actual != "oinv_A" {
		t.Errorf("check=%+v", failed)
	}
}

func TestWebhookWaitExpect_FailsWhenMaxIsExceeded(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.accepted", "oinv_A"),
		event("req-2", "organization.invitation.accepted", "oinv_A"),
	})
	group := invitationGroup("Accepted once", "organization.invitation.accepted", "{{invite-a.id}}")
	group.Max = new(1)
	wait := decodeExpectWait(t, 5000, 0, []expectGroup{group})

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if err == nil {
		t.Fatal("expected failure")
	}
	if res.Expectations[0].Problem != "2 arrived" {
		t.Errorf("problem=%q", res.Expectations[0].Problem)
	}
	if res.DurationMs > 2000 {
		t.Errorf("a broken group should end the wait early, took %dms", res.DurationMs)
	}
}

func TestWebhookWaitExpect_NeverGroupPassesWhenNothingArrives(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.deleted", "oinv_B"),
	})
	group := invitationGroup("No accept after withdrawal", "organization.invitation.accepted", "{{invite-b.id}}")
	group.Min, group.Max = new(0), new(0)
	wait := decodeExpectWait(t, 2000, 0, []expectGroup{group})

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if err != nil || !res.Expectations[0].Passed {
		t.Fatalf("err=%v expectations=%+v", err, res.Expectations)
	}
}

func TestWebhookWaitExpect_SettleCatchesALateExtraEvent(t *testing.T) {
	accepted := event("req-1", "organization.invitation.accepted", "oinv_A")
	url := serveBatches(t,
		[]map[string]any{accepted},
		[]map[string]any{accepted, event("req-2", "organization.invitation.accepted", "oinv_A")},
	)
	group := invitationGroup("Accepted once", "organization.invitation.accepted", "{{invite-a.id}}")
	group.Max = new(1)
	wait := decodeExpectWait(t, 5000, 1000, []expectGroup{group})

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if err == nil || res.Expectations[0].Problem != "2 arrived" {
		t.Fatalf("err=%v expectations=%+v", err, res.Expectations)
	}
}

func TestWebhookWaitExpect_EveryEventChecksRunOnClaimedEvents(t *testing.T) {
	unsigned := event("req-1", "organization.invitation.created", "oinv_A")
	unsigned["headers"] = map[string]string{}
	url := serveBatches(t, []map[string]any{unsigned})
	wait := decodeExpectWait(t, 2000, 0,
		[]expectGroup{invitationGroup("A created", "organization.invitation.created", "{{invite-a.id}}")},
		headerStartsWith("webhook-signature", "v1,"),
	)

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if err == nil {
		t.Fatal("expected the unsigned event to fail")
	}
	if !strings.Contains(res.Expectations[0].Problem, "webhook-signature") {
		t.Errorf("problem=%q", res.Expectations[0].Problem)
	}
}

func TestWebhookWaitExpect_UnresolvedReferenceFailsOnlyItsGroup(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.created", "oinv_A"),
	})
	wait := decodeExpectWait(t, 2000, 0, []expectGroup{
		invitationGroup("A created", "organization.invitation.created", "{{invite-a.id}}"),
		invitationGroup("C created", "organization.invitation.created", "{{invite-c.id}}"),
	})

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if err == nil {
		t.Fatal("expected the unevaluated group to fail the wait")
	}
	if !res.Expectations[0].Passed {
		t.Errorf("first group should still be evaluated: %+v", res.Expectations[0])
	}
	if res.Expectations[1].Problem != "not evaluated: no value for {{invite-c.id}}" {
		t.Errorf("problem=%q", res.Expectations[1].Problem)
	}
}

func TestWebhookWaitExpect_RejectsAGroupWithoutAName(t *testing.T) {
	wait := decodeExpectWait(t, 2000, 0, []expectGroup{
		invitationGroup("", "organization.invitation.created", "{{invite-a.id}}"),
	})

	_, err := runExpectWait(t, wait, "http://example.invalid", inviteOutputs)
	if spi.ErrorCode(err) != "WEBHOOK_WAIT_FAILED" {
		t.Fatalf("code=%s", spi.ErrorCode(err))
	}
}

func TestWebhookWait_ResolvesTemplatesInAssertionValues(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.created", "oinv_B"),
		event("req-2", "organization.invitation.created", "oinv_A"),
	})
	raw, _ := json.Marshal(map[string]any{
		"id":         "wait",
		"type":       "webhook_wait",
		"assertions": []map[string]any{bodyEquals("$.data.invitation_id", "{{invite-a.id}}")},
		"data":       map[string]any{"timeout_ms": 2000},
	})
	wait := decodeWebhookWait(t, raw)

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.Outputs["id"] != "req-2" {
		t.Errorf("id=%v", res.Outputs["id"])
	}
}

func TestWebhookWait_DeclaresAssertionReferencesForValidation(t *testing.T) {
	wait := decodeExpectWait(t, 2000, 0,
		[]expectGroup{invitationGroup("A created", "organization.invitation.created", "{{invite-a.id}}")},
		bodyEquals("$.data.organization_id", "{{create-org.orgId}}"),
	)

	refs := strings.Join(wait.ResolvedReferences(), ",")
	if !strings.Contains(refs, "invite-a.id") || !strings.Contains(refs, "create-org.orgId") {
		t.Errorf("refs=%s", refs)
	}
	if len(wait.InputSchema()) != 0 {
		t.Errorf("a wait must not require its refs as inputs: %v", wait.InputSchema())
	}
}

func TestWebhookWaitExpect_UnresolvedCheckOnEveryEventFailsEveryGroup(t *testing.T) {
	url := serveBatches(t, []map[string]any{
		event("req-1", "organization.invitation.created", "oinv_A"),
	})
	wait := decodeExpectWait(t, 2000, 0,
		[]expectGroup{invitationGroup("A created", "organization.invitation.created", "{{invite-a.id}}")},
		bodyEquals("$.data.organization_id", "{{create-org.orgId}}"),
	)

	res, err := runExpectWait(t, wait, url, inviteOutputs)
	if spi.ErrorCode(err) != "WEBHOOK_WAIT_EXPECTATIONS_FAILED" {
		t.Fatalf("code=%s err=%v", spi.ErrorCode(err), err)
	}
	if res.Expectations[0].Problem != "not evaluated: no value for {{create-org.orgId}}" {
		t.Errorf("problem=%q", res.Expectations[0].Problem)
	}
}

func TestWebhookWait_ReferenceWithNoValueIsAFailedAssertion(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"id":         "wait",
		"type":       "webhook_wait",
		"run_when":   "always",
		"assertions": []map[string]any{bodyEquals("$.data.invitation_id", "{{invite-c.id}}")},
		"data":       map[string]any{"timeout_ms": 2000},
	})
	wait := decodeWebhookWait(t, raw)

	_, err := runExpectWait(t, wait, "http://example.invalid", inviteOutputs)
	if spi.ErrorCode(err) != "ASSERTION_FAILED" {
		t.Fatalf("code=%s err=%v", spi.ErrorCode(err), err)
	}
}

func TestWebhookWaitExpect_GroupAssertsOnAQueryParam(t *testing.T) {
	search := event("req-1", "search", "oinv_A")
	search["query_params"] = map[string]string{"q": "x"}
	url := serveBatches(t, []map[string]any{event("req-0", "search", "oinv_A"), search})
	wait := decodeExpectWait(t, 2000, 0, []expectGroup{{
		Name: "searched for x",
		Assertions: []map[string]any{{
			"extractor_type": "queryParam",
			"extractor_data": map[string]any{"param_name": "q"},
			"operator_type":  "equals",
			"operator_data":  map[string]any{"value": "x"},
		}},
	}})

	res, err := runExpectWait(t, wait, url, nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got := res.Expectations[0].RequestIDs; len(got) != 1 || got[0] != "req-1" {
		t.Errorf("request ids=%v", got)
	}
}
