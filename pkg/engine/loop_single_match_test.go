package engine_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A json_path wildcard that matches one element yields that element, not a
// one-element array; a loop over it still runs once with that element.
func TestLoopIteratesASingleWildcardMatchAsOneItem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"a1"}]}`))
	}))
	defer server.Close()

	outputs := executeFlowJSON(t, `{
		"name": "f", "version": "1.0",
		"nodes": [
			{"id": "one", "type": "request",
				"data": {"method": "GET", "url": "`+server.URL+`/items", "timeout": 1000},
				"outputs": [{"name": "items", "extractor": {"type": "json_path", "path": "$.items[*]"}}]},
			{"id": "loop", "type": "loop", "data": {
				"items": "{{one.items}}",
				"body": {"nodes": [{"id": "pick", "type": "set_variable",
					"data": {"variables": {"account_id": "{{item.id}}"}}}], "edges": []}
			}}
		],
		"edges": [{"id": "e1", "source": "one", "target": "loop"}]
	}`, map[string]any{})

	assert.Equal(t, []map[string]any{{"pick.account_id": "a1"}}, outputs["loop"].(map[string]any)["results"])
}
