package node_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

// fakeCtx implements the ResponseContext capability interfaces used by the
// statusCode and jsonPath extractors.
type fakeCtx struct {
	status int
	parsed interface{}
	raw    []byte
}

func (c fakeCtx) HasCapability(string) bool  { return true }
func (c fakeCtx) GetStatus() int             { return c.status }
func (c fakeCtx) GetHeader(string) string    { return "" }
func (c fakeCtx) Headers() http.Header       { return http.Header{} }
func (c fakeCtx) GetParsedBody() interface{} { return c.parsed }
func (c fakeCtx) GetRawBody() []byte         { return c.raw }
func (c fakeCtx) GetDuration() interface{}   { return nil }

var _ extractors.ResponseContext = fakeCtx{}

// mkAssertion builds a CompositeAssertion from the backend's snake_case wire
// shape so tests exercise the real UnmarshalJSON path.
func mkAssertion(t *testing.T, extractor, path, op, value string) node.CompositeAssertion {
	t.Helper()
	ed := "{}"
	if path != "" {
		ed = fmt.Sprintf(`{"path":%q}`, path)
	}
	od := "{}"
	if value != "" {
		od = fmt.Sprintf(`{"value":%q}`, value)
	}
	raw := fmt.Sprintf(
		`{"extractor_type":%q,"extractor_data":%s,"operator_type":%q,"operator_data":%s}`,
		extractor, ed, op, od,
	)
	var ca node.CompositeAssertion
	if err := json.Unmarshal([]byte(raw), &ca); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return ca
}

func TestEvaluate_StatusCode(t *testing.T) {
	ca := mkAssertion(t, "statusCode", "", "equals", "201")
	if ok, err := ca.Evaluate(fakeCtx{status: 201}); err != nil || !ok {
		t.Fatalf("expected pass, got ok=%v err=%v", ok, err)
	}
	if bad, err := ca.Evaluate(fakeCtx{status: 400}); err != nil || bad {
		t.Fatalf("expected fail on 400, got ok=%v err=%v", bad, err)
	}
}

func TestEvaluate_JSONPathOperators(t *testing.T) {
	body := map[string]interface{}{"name": "eptest", "id": "prd_123", "empty": ""}
	cases := []struct {
		extractor, path, op, value string
		want                       bool
	}{
		{"jsonPath", "$.name", "equals", "eptest", true},
		{"jsonPath", "$.name", "equals", "nope", false},
		{"jsonPath", "$.id", "notEmpty", "", true},
		{"jsonPath", "$.empty", "empty", "", true},
		{"jsonPath", "$.name", "contains", "test", true},
	}
	for i, c := range cases {
		ca := mkAssertion(t, c.extractor, c.path, c.op, c.value)
		got, err := ca.Evaluate(fakeCtx{status: 200, parsed: body})
		if err != nil {
			t.Fatalf("case %d: err %v", i, err)
		}
		if got != c.want {
			t.Fatalf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func TestEvaluate_NumericCompare(t *testing.T) {
	ca := mkAssertion(t, "statusCode", "", "greaterThan", "200")
	if ok, err := ca.Evaluate(fakeCtx{status: 201}); err != nil || !ok {
		t.Fatalf("expected 201>200 pass, got ok=%v err=%v", ok, err)
	}
}

func TestEvaluateDetailed_ReturnsActual(t *testing.T) {
	ca := mkAssertion(t, "statusCode", "", "equals", "201")
	actual, passed, err := ca.EvaluateDetailed(fakeCtx{status: 201})
	if err != nil || !passed {
		t.Fatalf("expected pass, got passed=%v err=%v", passed, err)
	}
	if fmt.Sprint(actual) != "201" {
		t.Fatalf("expected actual=201, got %v", actual)
	}

	actual, passed, err = ca.EvaluateDetailed(fakeCtx{status: 400})
	if err != nil || passed {
		t.Fatalf("expected fail without error, got passed=%v err=%v", passed, err)
	}
	if fmt.Sprint(actual) != "400" {
		t.Fatalf("expected actual=400 on failure, got %v", actual)
	}
}

// reqNode builds a RequestNode carrying the given assertions for runAssertions tests.
func reqNode(assertions ...node.CompositeAssertion) *node.RequestNode {
	return &node.RequestNode{BaseNode: node.BaseNode{Assertions: assertions}}
}

func TestRunAssertions_RecordsEveryPass(t *testing.T) {
	n := reqNode(
		mkAssertion(t, "statusCode", "", "equals", "200"),
		mkAssertion(t, "jsonPath", "$.name", "equals", "eptest"),
	)
	ctx := fakeCtx{status: 200, parsed: map[string]interface{}{"name": "eptest"}}
	results, err := node.RunAssertionsForTest(n, ctx)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 recorded results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Passed {
			t.Errorf("result %d (%s %s) should have passed; actual=%v", i, r.Extractor, r.Operator, r.Actual)
		}
	}
	if fmt.Sprint(results[1].Actual) != "eptest" {
		t.Errorf("expected jsonPath actual=eptest, got %v", results[1].Actual)
	}
}

func TestRunAssertions_RecordsFailureWithActual(t *testing.T) {
	n := reqNode(
		mkAssertion(t, "statusCode", "", "equals", "200"),    // passes
		mkAssertion(t, "jsonPath", "$.name", "equals", "no"), // fails: actual "eptest"
		mkAssertion(t, "jsonPath", "$.x", "equals", "y"),     // never reached
	)
	ctx := fakeCtx{status: 200, parsed: map[string]interface{}{"name": "eptest"}}
	results, err := node.RunAssertionsForTest(n, ctx)
	if err == nil {
		t.Fatal("expected a failure error")
	}
	// Stops at the first failure: 2 results recorded (pass + the failing one).
	if len(results) != 2 {
		t.Fatalf("expected 2 recorded results (pass + first failure), got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("result 0 should have passed")
	}
	fail := results[1]
	if fail.Passed {
		t.Error("result 1 should have failed")
	}
	// Actual is captured from the jsonPath extractor even on the failing assertion.
	if fmt.Sprint(fail.Actual) != "eptest" || fmt.Sprint(fail.Expected) != "no" {
		t.Errorf("expected actual=eptest expected=no, got actual=%v expected=%v", fail.Actual, fail.Expected)
	}
}

func TestRunAssertions_RecordsExtractorError(t *testing.T) {
	// jsonPath against a non-map parsed body makes the extractor error out.
	n := reqNode(mkAssertion(t, "jsonPath", "$.missing.deep", "equals", "x"))
	results, err := node.RunAssertionsForTest(n, fakeCtx{status: 200, parsed: "not-an-object"})
	if err == nil {
		t.Fatal("expected an extractor evaluation error")
	}
	if len(results) != 1 {
		t.Fatalf("expected the erroring assertion to be recorded, got %d results", len(results))
	}
	r := results[0]
	if r.Passed {
		t.Error("erroring assertion must not be marked passed")
	}
	if r.Error == "" {
		t.Error("expected the extractor error to be recorded in Error")
	}
	if r.Actual != nil {
		t.Errorf("expected nil actual on extractor error, got %v", r.Actual)
	}
}

// TestAssertionResults_SerializeInPayload proves the recorded results survive the
// marshal path used to build the run-output payload (interface -> concrete struct).
func TestAssertionResults_SerializeInPayload(t *testing.T) {
	n := reqNode(mkAssertion(t, "statusCode", "", "equals", "200"))
	results, err := node.RunAssertionsForTest(n, fakeCtx{status: 200})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Mirror how the result is held: an interface value carrying the concrete type.
	var held interface{} = &node.RequestExecutionResult{AssertionResults: results}
	encoded, err := json.Marshal(held)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"assertion_results"`) ||
		!strings.Contains(string(encoded), `"passed":true`) {
		t.Fatalf("assertion_results not serialized: %s", encoded)
	}
}
