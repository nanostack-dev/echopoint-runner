package node_test

import (
	"encoding/json"
	"fmt"
	"net/http"
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
