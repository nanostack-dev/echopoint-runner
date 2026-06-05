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
	if r := ca.Evaluate(fakeCtx{status: 201}); r.Error != "" || !r.Passed {
		t.Fatalf("expected pass, got %+v", r)
	}
	if r := ca.Evaluate(fakeCtx{status: 400}); r.Error != "" || r.Passed {
		t.Fatalf("expected clean fail on 400, got %+v", r)
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
		r := ca.Evaluate(fakeCtx{status: 200, parsed: body})
		if r.Error != "" {
			t.Fatalf("case %d: unexpected error %q", i, r.Error)
		}
		if r.Passed != c.want {
			t.Fatalf("case %d: got %v want %v", i, r.Passed, c.want)
		}
	}
}

func TestEvaluate_NumericCompare(t *testing.T) {
	ca := mkAssertion(t, "statusCode", "", "greaterThan", "200")
	if r := ca.Evaluate(fakeCtx{status: 201}); r.Error != "" || !r.Passed {
		t.Fatalf("expected 201>200 pass, got %+v", r)
	}
}

// TestEvaluate_ExpandedOperators covers the operators the compatibility matrix
// declares but the evaluator previously did not implement (startsWith, endsWith,
// regex, greaterThanOrEqual, lessThanOrEqual).
func TestEvaluate_ExpandedOperators(t *testing.T) {
	body := map[string]interface{}{"name": "eptest"}
	cases := []struct {
		name                       string
		extractor, path, op, value string
		status                     int
		want                       bool
	}{
		{"gte pass", "statusCode", "", "greaterThanOrEqual", "200", 200, true},
		{"gte fail", "statusCode", "", "greaterThanOrEqual", "201", 200, false},
		{"lte pass", "statusCode", "", "lessThanOrEqual", "299", 200, true},
		{"lte fail", "statusCode", "", "lessThanOrEqual", "199", 200, false},
		{"startsWith pass", "jsonPath", "$.name", "startsWith", "ep", 200, true},
		{"startsWith fail", "jsonPath", "$.name", "startsWith", "zz", 200, false},
		{"endsWith pass", "jsonPath", "$.name", "endsWith", "test", 200, true},
		{"regex pass", "jsonPath", "$.name", "regex", "^ep.*t$", 200, true},
		{"regex fail", "jsonPath", "$.name", "regex", "^x", 200, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ca := mkAssertion(t, c.extractor, c.path, c.op, c.value)
			r := ca.Evaluate(fakeCtx{status: c.status, parsed: body})
			if r.Error != "" {
				t.Fatalf("unexpected error: %s", r.Error)
			}
			if r.Passed != c.want {
				t.Fatalf("got passed=%v want %v", r.Passed, c.want)
			}
		})
	}
}

func TestEvaluate_Between(t *testing.T) {
	// between needs operator_data.value to be a [min, max] array.
	raw := `{"extractor_type":"statusCode","extractor_data":{},"operator_type":"between","operator_data":{"value":[200,299]}}`
	var ca node.CompositeAssertion
	if err := json.Unmarshal([]byte(raw), &ca); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r := ca.Evaluate(fakeCtx{status: 250}); r.Error != "" || !r.Passed {
		t.Fatalf("expected 250 in [200,299], got %+v", r)
	}
	if r := ca.Evaluate(fakeCtx{status: 500}); r.Error != "" || r.Passed {
		t.Fatalf("expected 500 outside [200,299], got %+v", r)
	}
}

func TestEvaluate_UnknownOperator(t *testing.T) {
	ca := mkAssertion(t, "statusCode", "", "definitelyNotAnOperator", "1")
	if r := ca.Evaluate(fakeCtx{status: 200}); r.Error == "" {
		t.Fatalf("expected an error for an unknown operator, got %+v", r)
	}
}

func TestEvaluate_CapturesActualAndMetadata(t *testing.T) {
	ca := mkAssertion(t, "statusCode", "", "equals", "201")

	pass := ca.Evaluate(fakeCtx{status: 201})
	if !pass.Passed || fmt.Sprint(pass.Actual) != "201" {
		t.Fatalf("expected pass with actual=201, got %+v", pass)
	}
	if pass.Extractor != "statusCode" || pass.Operator != "equals" || fmt.Sprint(pass.Expected) != "201" {
		t.Fatalf("expected metadata copied onto result, got %+v", pass)
	}

	fail := ca.Evaluate(fakeCtx{status: 400})
	if fail.Passed || fail.Error != "" || fmt.Sprint(fail.Actual) != "400" {
		t.Fatalf("expected clean fail capturing actual=400, got %+v", fail)
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
