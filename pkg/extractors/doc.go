// Package extractors pulls a value out of a node's result for assertions and
// outputs: jsonPath/xmlPath into a parsed body, the raw body, an HTTP status
// code, a header.
//
// One typed extractor per type behind the AnyExtractor interface, dispatched
// through a registry: RegisterExtractor binds an ExtractorType to a factory that
// decodes the extractor's JSON config into its concrete struct, and
// UnmarshalExtractor peeks the wire "type" field to pick the factory. Adding an
// extractor = a new struct + RegisterExtractor from an init() — no switch to
// edit. Built-ins register in builtin.go; the HTTP-only ones (statusCode/header)
// register from the extractors/http subpackage.
//
// Extract returns any: the extracted value's type is only known at runtime (a
// JSONPath into an arbitrary body), so this is the deliberate untyped boundary.
package extractors
