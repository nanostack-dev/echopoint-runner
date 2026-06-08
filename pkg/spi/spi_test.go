package spi_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/executionevents"
	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// These helpers compile only if each spi name and its source name are the *same*
// type (a Go alias): they take one and return the other with no conversion.
// Named defined types with identical underlying types are not assignable to one
// another, so a non-alias would fail to build here — that is the assertion.
func asKind(k spi.Kind) node.Type                                  { return k }
func asNodeType(t node.Type) spi.Kind                              { return t }
func asRunWhen(w spi.RunWhen) node.RunWhen                         { return w }
func asExtractorType(e spi.ExtractorType) extractors.ExtractorType { return e }
func asEventType(v spi.EventType) executionevents.Type             { return v }

// TestAliasIdentity is a compile-time assertion (the body always passes); the
// point is that it builds at all.
func TestAliasIdentity(_ *testing.T) {
	_ = asKind(node.TypeRequest)
	_ = asNodeType(spi.KindDelay)
	_ = asRunWhen(node.RunWhenAlways)
	_ = asExtractorType(extractors.ExtractorTypeHeader)
	_ = asEventType(executionevents.NodeFailed)

	// The interface aliases exist and are usable.
	var _ spi.Node
	var _ spi.AnyResult
}

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
