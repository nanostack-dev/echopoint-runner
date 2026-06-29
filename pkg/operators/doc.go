// Package operators evaluates assertion operators (equals, contains, greaterThan,
// between, ...) against an extracted actual value and the assertion's expected
// value.
//
// One Comparator per operator type, dispatched through a registry: comparators
// maps an OperatorType to a func(actual, expected any) (bool, error). Compare
// looks up and applies it; IsKnown reports whether a type is registered (used to
// reject a bad operator at flow-decode rather than at execution). Adding an
// operator = register a Comparator (built-ins in registry.go; external ones via
// Register from an init()) — no switch to edit.
//
// Comparisons are deliberately lenient: string operators compare stringified
// forms (so 200 == "200") and numeric operators coerce via toFloat. This mirrors
// the wire, which delivers expected values as strings. It is a tested contract,
// not type-aware equality — see Compare and registry_test.go.
package operators
