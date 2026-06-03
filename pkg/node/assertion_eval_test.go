package node

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
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

func unmarshalAssertion(t *testing.T, raw string) CompositeAssertion {
	t.Helper()
	var ca CompositeAssertion
	if err := json.Unmarshal([]byte(raw), &ca); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return ca
}

func TestEvaluate_SnakeCaseStatusCode(t *testing.T) {
	ca := unmarshalAssertion(t, `{"operator_type":"equals","operator_data":{"value":"201"},"extractor_type":"statusCode","extractor_data":{}}`)
	ok, err := ca.Evaluate(fakeCtx{status: 201})
	if err != nil || !ok {
		t.Fatalf("expected pass, got ok=%v err=%v", ok, err)
	}
	bad, err := ca.Evaluate(fakeCtx{status: 400})
	if err != nil || bad {
		t.Fatalf("expected fail on 400, got ok=%v err=%v", bad, err)
	}
}

func TestEvaluate_JSONPathOperators(t *testing.T) {
	body := map[string]interface{}{"name": "eptest", "id": "prd_123", "empty": ""}
	cases := []struct {
		raw  string
		want bool
	}{
		{`{"operator_type":"equals","operator_data":{"value":"eptest"},"extractor_type":"jsonPath","extractor_data":{"path":"$.name"}}`, true},
		{`{"operator_type":"equals","operator_data":{"value":"nope"},"extractor_type":"jsonPath","extractor_data":{"path":"$.name"}}`, false},
		{`{"operator_type":"notEmpty","operator_data":{},"extractor_type":"jsonPath","extractor_data":{"path":"$.id"}}`, true},
		{`{"operator_type":"empty","operator_data":{},"extractor_type":"jsonPath","extractor_data":{"path":"$.empty"}}`, true},
		{`{"operator_type":"contains","operator_data":{"value":"test"},"extractor_type":"jsonPath","extractor_data":{"path":"$.name"}}`, true},
	}
	for i, c := range cases {
		ca := unmarshalAssertion(t, c.raw)
		got, err := ca.Evaluate(fakeCtx{status: 200, parsed: body})
		if err != nil {
			t.Fatalf("case %d: err %v", i, err)
		}
		if got != c.want {
			t.Fatalf("case %d: got %v want %v (%s)", i, got, c.want, c.raw)
		}
	}
}

func TestEvaluate_NumericCompare(t *testing.T) {
	ca := unmarshalAssertion(t, `{"operator_type":"greaterThan","operator_data":{"value":"200"},"extractor_type":"statusCode","extractor_data":{}}`)
	ok, err := ca.Evaluate(fakeCtx{status: 201})
	if err != nil || !ok {
		t.Fatalf("expected 201>200 pass, got ok=%v err=%v", ok, err)
	}
}
