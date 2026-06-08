// Package spi is the public contract — the "service provider interface" — of
// echopoint-runner: the wire-facing types that downstream control planes bind
// to. echopoint's openapi.yaml maps generated enums onto these via x-go-type,
// and its SSE decoders read results shaped by them, so the names and JSON
// values here are a cross-repo contract.
//
// Today spi is a thin facade that re-exports the canonical types through Go type
// aliases: spi.Kind is *the same type* as the underlying node.Type (an alias,
// not a conversion), so binding to spi is byte-for-byte wire-stable and
// behavior-identical to binding to the source packages directly.
//
// The deferred v2 (see docs/runner-api-redesign.md) inverts the dependency so
// spi becomes the L0 leaf that engine/kinds/extractors depend on, rather than a
// facade over them. Until then, these aliases give consumers a single, stable
// contract package to import and let the spec reference one package instead of
// reaching into node/extractors/executionevents.
package spi

import (
	"github.com/nanostack-dev/echopoint-runner/pkg/executionevents"
	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

// Kind identifies a node kind on the wire (was node.Type). The registry, not a
// fixed set, decides which kinds are valid; the constants below are the built-ins.
type Kind = node.Type

// Built-in node kinds.
const (
	KindRequest = node.TypeRequest
	KindDelay   = node.TypeDelay
	KindModule  = node.TypeModule
)

// RunWhen controls whether a node runs only on the success path or also after
// the main phase has already failed (was node.RunWhen).
type RunWhen = node.RunWhen

// RunWhen phases.
const (
	RunWhenOnSuccess = node.RunWhenOnSuccess
	RunWhenAlways    = node.RunWhenAlways
)

// Node is the engine's polymorphic view of any flow node (was node.AnyNode).
type Node = node.AnyNode

// AnyResult is the polymorphic execution result of any node, carrying the
// wire-stable BaseExecutionResult fields (was node.AnyExecutionResult).
type AnyResult = node.AnyExecutionResult

// ExtractorType identifies an output/assertion extractor on the wire
// (was extractors.ExtractorType).
type ExtractorType = extractors.ExtractorType

// Built-in extractor types.
const (
	ExtractorTypeJSONPath   = extractors.ExtractorTypeJSONPath
	ExtractorTypeXMLPath    = extractors.ExtractorTypeXMLPath
	ExtractorTypeStatusCode = extractors.ExtractorTypeStatusCode
	ExtractorTypeHeader     = extractors.ExtractorTypeHeader
	ExtractorTypeBody       = extractors.ExtractorTypeBody
)

// EventType identifies a runner execution/progress event on the wire
// (was executionevents.Type).
type EventType = executionevents.Type

// Execution/progress event types.
const (
	EventFlowStarted   = executionevents.FlowStarted
	EventNodeStarted   = executionevents.NodeStarted
	EventNodeCompleted = executionevents.NodeCompleted
	EventNodeFailed    = executionevents.NodeFailed
	EventFlowCompleted = executionevents.FlowCompleted
	EventFlowFailed    = executionevents.FlowFailed
)
