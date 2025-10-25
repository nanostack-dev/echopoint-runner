package it_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/it/shared"
)

func TestSharedSetup(t *testing.T) {
	ctx := shared.GetFlowEngineContext()
	if ctx == nil { //nolint:staticcheck // context initialized in TestMain
		t.Fatal("test context should be initialized by LaunchTest")
	}

	if ctx.WireMockURL == "" { //nolint:staticcheck // context is checked above
		t.Fatal("WireMockURL should be set")
	}

	t.Logf("WireMock running at: %s", ctx.WireMockURL) //nolint:staticcheck // context is checked above
}

func TestMain(m *testing.M) {
	shared.LaunchTest(m)
}
