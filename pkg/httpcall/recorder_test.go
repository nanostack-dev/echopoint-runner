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

func TestStart_WithoutLimiterOrCollectorRecordsNothing(t *testing.T) {
	call, err := httpcall.Start(context.Background(), httpcall.Target{URL: "https://example.com"})

	require.NoError(t, err)
	call.Finish(httpcall.Outcome{StatusCode: 200})
	var unstarted *httpcall.Call
	unstarted.Finish(httpcall.Outcome{})
}

func TestLimiter_RefusesTheCallPastTheLimit(t *testing.T) {
	ctx := httpcall.WithLimiter(context.Background(), httpcall.NewLimiter(1))

	_, err := httpcall.Start(ctx, httpcall.Target{URL: "https://example.com"})
	require.NoError(t, err)
	_, err = httpcall.Start(ctx, httpcall.Target{URL: "https://example.com"})

	userErr, ok := spi.AsUserError(err)
	require.True(t, ok)
	assert.Equal(t, httpcall.LimitExceededCode, userErr.Code)
}

func TestLimiter_ZeroLimitNeverRefuses(t *testing.T) {
	ctx := httpcall.WithLimiter(context.Background(), httpcall.NewLimiter(0))
	for range 1000 {
		_, err := httpcall.Start(ctx, httpcall.Target{URL: "https://example.com"})
		require.NoError(t, err)
	}
}

func TestCollector_GroupsCallsByNodeAndTarget(t *testing.T) {
	collector := httpcall.NewCollector()
	ctx := httpcall.WithCollector(context.Background(), collector)
	item := httpcall.Target{
		NodeID:      "get_item",
		Method:      "get",
		URL:         "https://API.acme.com:8443/items/1",
		URLTemplate: "{{base}}/items/{{id}}",
	}
	login := httpcall.Target{
		NodeID:      "login",
		Method:      "POST",
		URL:         "https://auth.acme.com/token",
		URLTemplate: "{{auth}}/token",
	}

	for _, step := range []struct {
		target  httpcall.Target
		outcome httpcall.Outcome
	}{
		{item, httpcall.Outcome{StatusCode: 200, ResponseBytes: new(int64(10))}},
		{login, httpcall.Outcome{StatusCode: 201, ResponseBytes: new(int64(5))}},
		{item, httpcall.Outcome{StatusCode: 404, ResponseBytes: new(int64(2))}},
		{item, httpcall.Outcome{TransportError: spi.HTTPCallErrorTimeout, ResponseBytes: new(int64(0))}},
	} {
		call, err := httpcall.Start(ctx, step.target)
		require.NoError(t, err)
		call.Finish(step.outcome)
	}

	groups := collector.Groups()
	require.Len(t, groups, 2)
	items, logins := groups[0], groups[1]
	assert.Equal(t, "get_item", items.NodeID)
	assert.Equal(t, "GET", items.Method)
	assert.Equal(t, "api.acme.com:8443", items.Host)
	assert.Equal(t, "/items/{id}", items.PathTemplate)
	assert.Equal(t, 3, items.Count())
	assert.Equal(t, []int{200, 404, 0}, items.StatusCodes)
	assert.Equal(t, []int64{10, 2, 0}, items.ResponseBytes)
	assert.Equal(t, map[string]spi.HTTPCallErrorClass{"2": spi.HTTPCallErrorTimeout}, items.TransportErrors)
	assert.Equal(t, int64(0), items.OffsetsMs[0])
	assert.Equal(t, "login", logins.NodeID)
	assert.Equal(t, []int{201}, logins.StatusCodes)
}

func TestCollector_StreamedCallsHaveNoResponseBytes(t *testing.T) {
	collector := httpcall.NewCollector()
	ctx := httpcall.WithCollector(context.Background(), collector)

	call, err := httpcall.Start(ctx, httpcall.Target{NodeID: "watch", URL: "https://example.com/events"})
	require.NoError(t, err)
	call.Finish(httpcall.Outcome{StatusCode: 200})

	groups := collector.Groups()
	require.Len(t, groups, 1)
	assert.Nil(t, groups[0].ResponseBytes)
}

func TestCollector_IsSafeForParallelNodes(t *testing.T) {
	collector := httpcall.NewCollector()
	ctx := httpcall.WithCollector(context.Background(), collector)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			call, err := httpcall.Start(ctx, httpcall.Target{NodeID: "get", URL: "https://example.com"})
			if err != nil {
				t.Error(err)
			}
			call.Finish(httpcall.Outcome{StatusCode: 200})
		})
	}
	wg.Wait()

	groups := collector.Groups()
	require.Len(t, groups, 1)
	assert.Equal(t, 50, groups[0].Count())
}
