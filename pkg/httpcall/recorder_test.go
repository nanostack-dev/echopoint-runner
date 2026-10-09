package httpcall_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/pkg/httpcall"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func TestPathTemplate(t *testing.T) {
	cases := map[string]string{
		"{{baseUrl}}/users/{{ userId }}?expand=true": "/users/{userId}",
		"{{ baseUrl }}": "/",
		"https://api.example.com/orders/{{id}}#section": "/orders/{id}",
		"https://api.example.com":                       "/",
		"https://user:pass@api.example.com/v1/items":    "/v1/items",
		"/relative/{{id}}":                              "/relative/{id}",
		"{{baseUrl}}/search/{{ q ? 'a' : 'b' }}":        "/search/{q ? 'a' : 'b'}",
		"  {{baseUrl}}/trimmed  ":                       "/trimmed",
	}
	for template, want := range cases {
		assert.Equal(t, want, httpcall.PathTemplate(template), template)
	}
}

func TestPathTemplate_CapsLength(t *testing.T) {
	long := "/" + string(make([]byte, 600))
	assert.Len(t, httpcall.PathTemplate(long), 512)
}

func TestStart_WithoutRecorderRecordsNothing(t *testing.T) {
	call, err := httpcall.Start(context.Background(), httpcall.Target{URL: "https://example.com"})

	require.NoError(t, err)
	call.Finish(httpcall.Outcome{})
	var unstarted *httpcall.Call
	unstarted.Finish(httpcall.Outcome{})
}

func TestStart_RefusesThePastLimitCall(t *testing.T) {
	recorder := httpcall.NewRecorder(1)
	ctx := httpcall.WithRecorder(context.Background(), recorder)

	first, err := httpcall.Start(ctx, httpcall.Target{NodeID: "a", Method: "post", URL: "https://Example.com:8443/x"})
	require.NoError(t, err)
	status := 503
	first.Finish(httpcall.Outcome{StatusCode: &status})

	_, err = httpcall.Start(ctx, httpcall.Target{NodeID: "b", URL: "https://example.com"})
	userErr, ok := spi.AsUserError(err)
	require.True(t, ok)
	assert.Equal(t, httpcall.LimitExceededCode, userErr.Code)

	calls := recorder.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "POST", calls[0].Method)
	assert.Equal(t, "example.com:8443", calls[0].Host)
	assert.Equal(t, spi.HTTPCallError5xx, calls[0].ErrorClass)
}

func TestRecorder_ZeroLimitRecordsEveryCall(t *testing.T) {
	recorder := httpcall.NewRecorder(0)
	ctx := httpcall.WithRecorder(context.Background(), recorder)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			call, err := httpcall.Start(ctx, httpcall.Target{URL: "https://example.com"})
			if err != nil {
				t.Error(err)
			}
			call.Finish(httpcall.Outcome{ErrorClass: spi.HTTPCallErrorTimeout})
		})
	}
	wg.Wait()

	calls := recorder.Calls()
	require.Len(t, calls, 50)
	for i, call := range calls {
		assert.Equal(t, i+1, call.Seq)
	}
}
