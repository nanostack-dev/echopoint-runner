package spi_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// TestWireValues locks the JSON string values: these cross the wire to
// echopoint's openapi enums and SSE decoders and must never drift.
func TestWireValues(t *testing.T) {
	cases := map[string]string{
		string(spi.KindRequest):           "request",
		string(spi.KindDelay):             "delay",
		string(spi.KindModule):            "module",
		string(spi.RunWhenOnSuccess):      "on_success",
		string(spi.RunWhenAlways):         "always",
		string(spi.ExtractorTypeJSONPath): "jsonPath",
		string(spi.ExtractorTypeXMLPath):  "xmlPath",
		string(spi.ExtractorTypeBody):     "body",
		string(spi.EventNodeFailed):       "node.failed",
		string(spi.EventFlowCompleted):    "flow.completed",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("wire value drift: got %q want %q", got, want)
		}
	}
}
