package node_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	node "github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestNodeDoesNotFollowASecretToAnotherHost(t *testing.T) {
	var sawToken atomic.Bool
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawToken.Store(r.Header.Get("X-Token") == "s3cret")
		http.Redirect(w, r, "https://evil.example/landed", http.StatusFound)
	}))
	t.Cleanup(first.Close)

	_, err := secretRequest(first.URL + "/start").Execute(secretContext(t, first.URL))

	require.Error(t, err)
	var userErr *spi.UserError
	require.ErrorAs(t, err, &userErr)
	assert.Equal(t, "SECRET_HOST_NOT_ALLOWED", userErr.Code)
	assert.Contains(t, userErr.Message, "evil.example")
	assert.NotContains(t, err.Error(), "s3cret")
	assert.True(t, sawToken.Load())
}

func TestRequestNodeDoesNotFollowARedirectThatPutsTheSecretInTheURL(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/landed?token=s3cret", http.StatusFound)
	}))
	t.Cleanup(first.Close)

	_, err := secretRequest(first.URL + "/start").Execute(secretContext(t, first.URL))

	require.Error(t, err)
	var userErr *spi.UserError
	require.ErrorAs(t, err, &userErr)
	assert.Equal(t, "SECRET_IN_URL", userErr.Code)
	assert.NotContains(t, err.Error(), "s3cret")
}

func TestRequestNodeFollowsARedirectToTheAllowedHost(t *testing.T) {
	var landed atomic.Bool
	var first *httptest.Server
	first = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, first.URL+"/done", http.StatusFound)
			return
		}
		landed.Store(r.Header.Get("X-Token") == "s3cret")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(first.Close)

	_, err := secretRequest(first.URL + "/start").Execute(secretContext(t, first.URL))

	require.NoError(t, err)
	assert.True(t, landed.Load())
}

func TestRequestNodeStillFollowsARedirectWhenNoSecretIsSent(t *testing.T) {
	var hits atomic.Int32
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(next.Close)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, next.URL+"/landed", http.StatusFound)
	}))
	t.Cleanup(first.Close)

	reqNode := &node.RequestNode{
		BaseNode: node.BaseNode{ID: "step", NodeType: spi.KindRequest},
		Data: node.RequestData{
			Method:  http.MethodGet,
			URL:     first.URL + "/start",
			Timeout: 2000,
		},
	}

	_, err := reqNode.Execute(spi.ExecutionContext{})

	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
}

func secretRequest(rawURL string) *node.RequestNode {
	return &node.RequestNode{
		BaseNode: node.BaseNode{ID: "step", NodeType: spi.KindRequest},
		Data: node.RequestData{
			Method:  http.MethodGet,
			URL:     rawURL,
			Headers: map[string]string{"X-Token": "{{apiToken}}"},
			Timeout: 2000,
		},
	}
}

func secretContext(t *testing.T, rawURL string) spi.ExecutionContext {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return spi.ExecutionContext{
		Inputs:       map[string]any{"apiToken": "s3cret"},
		SecretValues: map[string]string{"apiToken": "s3cret"},
		SecretHosts:  map[string][]string{"apiToken": {parsed.Hostname()}},
	}
}
