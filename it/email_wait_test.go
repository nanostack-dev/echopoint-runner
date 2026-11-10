package it_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testutil "github.com/nanostack-dev/echopoint-runner/it/shared"
	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Test_EmailWaitSignupFlow signs up with the run inbox address, waits for the
// verification email in a stubbed inbox read endpoint, and follows its link.
// The inbox serves an unrelated email first, and an HTML-only verification
// email whose link points back at WireMock.
func Test_EmailWaitSignupFlow(t *testing.T) {
	tc := testutil.GetFlowEngineContext()
	require.NotNil(t, tc, "test context should be initialized")

	flowDef := loadFlowFromJSON(t, "email-wait-signup-flow.json")
	flowDef.InitialInputs["apiUrl"] = tc.WireMockURL
	flowDef.InitialInputs["inbox.messages_url"] = tc.WireMockURL + "/runner/jobs/job-email/inbox-messages"

	flowEngine, err := engine.NewFlowEngine(*flowDef, &engine.Options{
		Ctx: spi.WithJobToken(context.Background(), "job-token-email"),
	})
	require.NoError(t, err)

	result, err := flowEngine.Execute(flowDef.InitialInputs)
	require.NoError(t, err)
	require.True(t, result.Success, "flow should succeed: %+v", result.ExecutionResults["verify"])

	verify, ok := spi.As[*node.EmailWaitExecutionResult](result.ExecutionResults["verify"])
	require.True(t, ok, "got %T", result.ExecutionResults["verify"])
	outputs := verify.GetOutputs()
	assert.Equal(t, "msg-verify", outputs["id"])
	assert.Equal(t, "482913", outputs["code"])
	assert.Equal(t, tc.WireMockURL+"/verify?token=abc123&u=1", outputs["first_link"])
	assert.Equal(t, tc.WireMockURL+"/verify?token=abc123&u=1", outputs["button"])
	assert.Contains(t, outputs["text"], "Your code is 482913.")
	assert.Len(t, verify.AssertionResults, 2)

	assert.Equal(t, true, result.FinalOutputs["confirm.verified"])
}

// Test_EmailWaitRejectedTokenFails reads the inbox with a token the endpoint
// refuses: the wait fails at once instead of polling to its timeout.
func Test_EmailWaitRejectedTokenFails(t *testing.T) {
	tc := testutil.GetFlowEngineContext()
	require.NotNil(t, tc)

	flowDef := loadFlowFromJSON(t, "email-wait-signup-flow.json")
	flowDef.InitialInputs["apiUrl"] = tc.WireMockURL
	flowDef.InitialInputs["inbox.messages_url"] = tc.WireMockURL + "/runner/jobs/job-email/inbox-messages"

	flowEngine, err := engine.NewFlowEngine(*flowDef, &engine.Options{
		Ctx: spi.WithJobToken(context.Background(), "wrong-token"),
	})
	require.NoError(t, err)

	result, _ := flowEngine.Execute(flowDef.InitialInputs)
	require.False(t, result.Success)
	verify, ok := spi.As[*node.EmailWaitExecutionResult](result.ExecutionResults["verify"])
	require.True(t, ok, "got %T", result.ExecutionResults["verify"])
	require.NotNil(t, verify.ErrorCode)
	assert.Equal(t, "EMAIL_WAIT_FAILED", *verify.ErrorCode)
}
