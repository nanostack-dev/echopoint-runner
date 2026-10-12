package runner_test

import (
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_HTTPCallLabelsPreserveSecretRedaction(t *testing.T) {
	secret := "MiXeDSecret"
	definition := parseFlow(t, `{"version":"1.0","name":"secret-host","nodes":[`+
		requestNode("get", "http://{{secret}}.invalid:1/path")+`],"edges":[]}`, "secret")
	result, _ := runner.Run(
		definition,
		map[string]any{"secret": secret},
		runner.WithSecretInputKeys([]string{"secret"}),
	)
	groups := httpCalls(t, result, "get")
	require.Len(t, groups, 1)
	assert.Equal(t, "***.invalid:1", groups[0].Host)

	server := statusServer(t)
	secret = strings.Repeat("s", 600)
	definition = parseFlow(t, `{"version":"1.0","name":"secret-path","nodes":[`+
		requestNode("get", server.URL+"/"+secret)+`],"edges":[]}`, "secret")
	result, err := runner.Run(
		definition,
		map[string]any{"secret": secret},
		runner.WithSecretInputKeys([]string{"secret"}),
	)
	require.NoError(t, err)
	groups = httpCalls(t, result, "get")
	require.Len(t, groups, 1)
	assert.Equal(t, "/***", groups[0].PathTemplate)
}
