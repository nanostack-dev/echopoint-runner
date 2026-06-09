// Package spi is the L0 contract — the "service provider interface" — of
// echopoint-runner. It owns the wire-facing and execution-result types that
// cross process and repo boundaries: echopoint's openapi.yaml binds its enums
// here via x-go-type, and the control plane decodes flow results shaped by these
// types over SSE and the database.
//
// spi depends on nothing inside echopoint-runner (only the standard library), so
// it sits at the bottom of the import graph: pkg/node, pkg/extractors,
// pkg/executionevents and pkg/engine all depend on spi, never the reverse. Those
// packages re-export the spi types as back-compat aliases (e.g.
// node.Type = spi.Kind), so existing call sites and the generated client keep
// compiling while spi remains the single source of truth for the contract.
//
// The JSON struct tags and enum string values in this package are a cross-repo
// contract; changing them is a breaking wire change.
package spi
