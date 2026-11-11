package node_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// messageCtx is a response context with every capability the message tests
// read: status, headers, parsed body and query parameters.
type messageCtx struct {
	status  int
	headers http.Header
	parsed  any
	query   map[string]string
}

func (c messageCtx) HasCapability(string) bool   { return true }
func (c messageCtx) GetStatus() int              { return c.status }
func (c messageCtx) GetHeader(key string) string { return c.headers.Get(key) }
func (c messageCtx) Headers() http.Header        { return c.headers }
func (c messageCtx) GetParsedBody() any          { return c.parsed }
func (c messageCtx) GetRawBody() []byte          { return nil }
func (c messageCtx) GetQueryParam(name string) (string, bool) {
	value, found := c.query[name]
	return value, found
}

// bareCtx has no capability, so every extractor fails to evaluate against it.
type bareCtx struct{}

func (bareCtx) HasCapability(string) bool { return false }

// mkTargetAssertion builds a CompositeAssertion from the backend's wire shape
// with free-form extractor data, so each extractor's own target key is decoded
// by the real UnmarshalJSON path.
func mkTargetAssertion(t *testing.T, extractor, extractorData, op, value string) node.CompositeAssertion {
	t.Helper()
	raw := fmt.Sprintf(
		`{"extractor_type":%q,"extractor_data":%s,"operator_type":%q,"operator_data":{"value":%q}}`,
		extractor, extractorData, op, value,
	)
	var ca node.CompositeAssertion
	require.NoError(t, json.Unmarshal([]byte(raw), &ca))
	return ca
}

// The failure message counts assertions from 1 and names what was checked,
// while the recorded result keeps its 0-based Index for the API and the UI.
func TestEvaluateAssertions_FailureMessage(t *testing.T) {
	passingStatus := func(t *testing.T) node.CompositeAssertion {
		t.Helper()
		return mkTargetAssertion(t, "status_code", `{}`, "equals", "200")
	}
	ctx := messageCtx{
		status:  200,
		headers: http.Header{"Content-Type": []string{"text/html"}},
		parsed:  map[string]any{"status": "available"},
		query:   map[string]string{"page": "2"},
	}

	cases := []struct {
		name       string
		assertions func(t *testing.T) []node.CompositeAssertion
		body       any // overrides the parsed body when set
		want       string
	}{
		{
			name: "first assertion failing prints 1",
			assertions: func(t *testing.T) []node.CompositeAssertion {
				t.Helper()
				return []node.CompositeAssertion{
					mkTargetAssertion(t, "status_code", `{}`, "equals", "404"),
				}
			},
			want: "assertion 1 failed: status_code equals expected=404 actual=200",
		},
		{
			name: "second assertion failing names its JSON path",
			assertions: func(t *testing.T) []node.CompositeAssertion {
				t.Helper()
				return []node.CompositeAssertion{
					passingStatus(t),
					mkTargetAssertion(t, "json_path", `{"path":"$.status"}`, "equals", "sold"),
				}
			},
			want: "assertion 2 failed: json_path $.status equals expected=sold actual=available",
		},
		{
			name: "header extractor names its header",
			assertions: func(t *testing.T) []node.CompositeAssertion {
				t.Helper()
				return []node.CompositeAssertion{
					mkTargetAssertion(t, "header", `{"header_name":"Content-Type"}`, "equals", "application/json"),
				}
			},
			want: "assertion 1 failed: header Content-Type equals expected=application/json actual=text/html",
		},
		{
			name: "query param extractor names its parameter",
			assertions: func(t *testing.T) []node.CompositeAssertion {
				t.Helper()
				return []node.CompositeAssertion{
					passingStatus(t),
					passingStatus(t),
					mkTargetAssertion(t, "query_param", `{"param_name":"page"}`, "equals", "3"),
				}
			},
			want: "assertion 3 failed: query_param page equals expected=3 actual=2",
		},
		{
			name: "extractor without a target prints extractor and operator only",
			assertions: func(t *testing.T) []node.CompositeAssertion {
				t.Helper()
				return []node.CompositeAssertion{
					passingStatus(t),
					mkTargetAssertion(t, "body", `{}`, "contains", "sold"),
				}
			},
			// A text body: the body extractor compares it as received.
			body: `{"status":"available"}`,
			want: `assertion 2 failed: body contains expected=sold actual={"status":"available"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertions := tc.assertions(t)
			caseCtx := ctx
			if tc.body != nil {
				caseCtx.parsed = tc.body
			}

			results, err := node.EvaluateAssertions(assertions, caseCtx)

			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
			assert.Equal(t, "ASSERTION_FAILED", spi.ErrorCode(err))
			require.Len(t, results, len(assertions))
			failed := results[len(results)-1]
			assert.False(t, failed.Passed)
			assert.Empty(t, failed.Error)
			assert.Equal(t, len(assertions)-1, failed.Index, "Index stays 0-based")
		})
	}
}

// An assertion that cannot be evaluated names itself the same way, in brackets,
// before the evaluation error.
func TestEvaluateAssertions_EvaluationErrorMessage(t *testing.T) {
	cases := []struct {
		name       string
		assertions func(t *testing.T) []node.CompositeAssertion
		ctx        extractors.ResponseContext
		wantPrefix string
		wantIndex  int
	}{
		{
			name: "second assertion with a JSON path",
			assertions: func(t *testing.T) []node.CompositeAssertion {
				t.Helper()
				return []node.CompositeAssertion{
					mkTargetAssertion(t, "status_code", `{}`, "equals", "200"),
					mkTargetAssertion(t, "json_path", `{"path":"$.status"}`, "equals", "sold"),
				}
			},
			ctx:        messageCtx{status: 200, parsed: "not-an-object"},
			wantPrefix: "assertion 2 (json_path $.status equals) evaluation error: ",
			wantIndex:  1,
		},
		{
			name: "missing header",
			assertions: func(t *testing.T) []node.CompositeAssertion {
				t.Helper()
				return []node.CompositeAssertion{
					mkTargetAssertion(t, "header", `{"header_name":"X-Request-Id"}`, "not_empty", ""),
				}
			},
			ctx:        messageCtx{status: 200, headers: http.Header{}},
			wantPrefix: "assertion 1 (header X-Request-Id not_empty) evaluation error: ",
		},
		{
			name: "first assertion without a target",
			assertions: func(t *testing.T) []node.CompositeAssertion {
				t.Helper()
				return []node.CompositeAssertion{
					mkTargetAssertion(t, "status_code", `{}`, "equals", "404"),
				}
			},
			ctx:        bareCtx{},
			wantPrefix: "assertion 1 (status_code equals) evaluation error: ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertions := tc.assertions(t)

			results, err := node.EvaluateAssertions(assertions, tc.ctx)

			require.Error(t, err)
			require.Len(t, results, len(assertions))
			failed := results[len(results)-1]
			require.NotEmpty(t, failed.Error)
			assert.Equal(t, tc.wantPrefix+failed.Error, err.Error())
			assert.Equal(t, "ASSERTION_FAILED", spi.ErrorCode(err))
			assert.Equal(t, tc.wantIndex, failed.Index, "Index stays 0-based")
		})
	}
}

// Target is what the message quotes: the extractor's own path, name, selector
// or pattern, and nothing for an extractor that reads the whole response.
func TestCompositeAssertion_Target(t *testing.T) {
	cases := []struct {
		extractor     string
		extractorData string
		want          string
	}{
		{"json_path", `{"path":"$.items[0].id"}`, "$.items[0].id"},
		{"xml_path", `{"path":"/order/status"}`, "/order/status"},
		{"header", `{"header_name":"Content-Type"}`, "Content-Type"},
		{"query_param", `{"param_name":"page"}`, "page"},
		{"html", `{"selector":"a.verify"}`, "a.verify"},
		{"html", `{"selector":"a.verify","attribute":"href"}`, "a.verify@href"},
		{"regex", `{"pattern":"code: (\\d+)"}`, `code: (\d+)`},
		{"status_code", `{}`, ""},
		{"body", `{}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.extractor+" "+tc.extractorData, func(t *testing.T) {
			ca := mkTargetAssertion(t, tc.extractor, tc.extractorData, "equals", "x")
			assert.Equal(t, tc.want, ca.Target())
		})
	}

	t.Run("no extractor", func(t *testing.T) {
		var ca node.CompositeAssertion
		assert.Empty(t, ca.Target())
	})
}
