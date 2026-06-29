# Operators

Evaluates assertion operators (`equals`, `contains`, `greaterThan`, `between`, …)
against an extracted **actual** value and the assertion's **expected** value.

## Model: one comparator per type, dispatched through a registry

```
OperatorType ("equals", "greaterThan", …)
        │
        ▼
  comparators map[OperatorType]Comparator      Comparator = func(actual, expected any) (bool, error)
        │
        ▼
  Compare(type, actual, expected) (bool, error)   // looks up + applies
  IsKnown(type) bool                              // is a comparator registered?
```

- Built-in comparators are registered in `registry.go`.
- Add an operator by registering a `Comparator` — built-ins inline, external ones
  via `Register(type, comparator)` from an `init()`. No dispatch switch to edit.
- `IsKnown` lets the decode layer reject an unknown operator at flow-parse time
  (mirroring the extractor registry) instead of failing inside `Compare` at run.

## Coercion contract (lenient, deliberate, tested)

Comparisons compare coerced forms, because the wire delivers expected values as
strings while extracted actuals are typed (e.g. an `int` status code vs `"200"`):

- **string operators** (`equals`, `notEquals`, `contains`, `startsWith`,
  `endsWith`, `regex`) compare `toString(actual)` vs `toString(expected)` — so
  `200` equals `"200"`. `equals` is string equality, **not** type-aware equality.
- **numeric operators** (`greaterThan`, `lessThan`, `…OrEqual`, `between`) coerce
  both sides via `toFloat`; non-numeric input is an error.
- **`empty` / `notEmpty`** inspect nil / empty string / empty list / empty map.

This contract is pinned by table tests in `registry_test.go`. Changing it (e.g.
making `equals` type-aware) is a behavior change visible to existing flows.

## Files

- `types.go` — `OperatorType` + the built-in constants.
- `registry.go` — the `comparators` registry, `Compare`, `IsKnown`, `Register`,
  and the coercion helpers (`toString`, `toFloat`, `compareNumeric`, `between`,
  `isEmpty`).
