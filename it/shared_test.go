package it_test

import (
	"testing"

	testutil "github.com/nanostack-dev/echopoint-flow-engine/it/shared"
)

func TestSharedSetup(t *testing.T) {
	ctx := testutil.GetFlowEngineContext()

	if ctx.WireMockURL == "" {
		t.Fatal("WireMockURL should be set")
	}

	t.Logf("WireMock running at: %s", ctx.WireMockURL)
}

func TestMain(m *testing.M) {
	testutil.LaunchTest(m)
}
