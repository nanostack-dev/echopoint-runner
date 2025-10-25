# Operators

This package provides a flexible, extensible operator system for validating values.

## Architecture: Two-Tier Hybrid Approach

The operator system uses a **two-tier architecture** that combines the Strategy pattern with type-safe factory methods:

```
┌────────────────────────────────────────────────────────┐
│              Operator Interface                        │
│  All operators implement: Validate(interface{}) bool  │
└────────────────────────────────────────────────────────┘
                         ▲
                         │
         ┌───────────────┼───────────────┐
         │               │               │
┌────────┴────────┐ ┌───┴────┐ ┌────────┴────────┐
│ String Operators│ │ Number │ │ Boolean         │
│ - Equals        │ │ Opera  │ │ Operators       │
│ - Contains      │ │ tors   │ │ - IsTrue        │
│ - StartsWith    │ │ - GT   │ │ - IsFalse       │
│ - Regex         │ │ - LT   │ │ - Equals        │
└─────────────────┘ └────────┘ └─────────────────┘

         ┌────────────────────┐
         │ Type-Safe Factories│
         │ StringOperators{}  │
         │ NumberOperators{}  │
         │ BooleanOperators{} │
         └────────────────────┘
```

## Benefits

1. **Operators are First-Class Citizens**: Each operator implements the `Operator` interface
2. **No Code Duplication**: Shared operators (like `Equals`) implemented once
3. **Type Safety**: Factory methods guide developers to valid operators for each type
4. **Extensible**: Add new operators without modifying existing code
5. **Self-Contained Type Checking**: Each operator validates its own input types

## Core Interface

```go
type Operator interface {
    Validate(actual interface{}) (bool, error)
    GetType() OperatorType
}
```

## Universal Operators

These operators work across multiple types:

### EqualsOperator
Checks if the actual value equals the expected value. Works with strings, numbers, and booleans.

```go
op := operators.EqualsOperator{Expected: "success"}
result, err := op.Validate("success") // true
```

### NotEqualsOperator
Inverse of EqualsOperator.

```go
op := operators.NotEqualsOperator{Expected: "error"}
result, err := op.Validate("success") // true
```

## String Operators

### ContainsOperator
Checks if a string contains a substring.

```go
op := operators.ContainsOperator{Substring: "world"}
result, err := op.Validate("hello world") // true
```

### StartsWithOperator
Checks if a string starts with a prefix.

```go
op := operators.StartsWithOperator{Prefix: "hello"}
result, err := op.Validate("hello world") // true
```

### EndsWithOperator
Checks if a string ends with a suffix.

```go
op := operators.EndsWithOperator{Suffix: "world"}
result, err := op.Validate("hello world") // true
```

### RegexOperator
Checks if a string matches a regular expression pattern.

```go
op := operators.RegexOperator{Pattern: `^[A-Z]{3}-\d{4}$`}
result, err := op.Validate("ABC-1234") // true
```

### EmptyOperator
Checks if a string is empty.

```go
op := operators.EmptyOperator{}
result, err := op.Validate("") // true
```

### NotEmptyOperator
Checks if a string is not empty.

```go
op := operators.NotEmptyOperator{}
result, err := op.Validate("hello") // true
```

### NotContainsOperator
Inverse of ContainsOperator.

```go
op := operators.NotContainsOperator{Substring: "error"}
result, err := op.Validate("success") // true
```

## Number Operators

### GreaterThanOperator
Checks if a number is greater than the expected value.

```go
op := operators.GreaterThanOperator{Expected: 100}
result, err := op.Validate(200) // true
```

### LessThanOperator
Checks if a number is less than the expected value.

```go
op := operators.LessThanOperator{Expected: 100}
result, err := op.Validate(50) // true
```

### GreaterThanOrEqualOperator
Checks if a number is greater than or equal to the expected value.

```go
op := operators.GreaterThanOrEqualOperator{Expected: 100}
result, err := op.Validate(100) // true
```

### LessThanOrEqualOperator
Checks if a number is less than or equal to the expected value.

```go
op := operators.LessThanOrEqualOperator{Expected: 100}
result, err := op.Validate(100) // true
```

### BetweenOperator
Checks if a number is between min and max (inclusive).

```go
op := operators.BetweenOperator{Min: 200, Max: 299}
result, err := op.Validate(250) // true
```

## Type-Safe Factories

Factory structs provide type-safe, discoverable APIs for creating operators:

### StringOperators

```go
str := operators.StringOperators{}

op := str.Equals("success")
op := str.Contains("substring")
op := str.StartsWith("prefix")
op := str.EndsWith("suffix")
op := str.Regex(`^pattern$`)
op := str.Empty()
op := str.NotEmpty()
op := str.NotEquals("error")
op := str.NotContains("substring")
```

### NumberOperators

```go
num := operators.NumberOperators{}

op := num.Equals(200)
op := num.NotEquals(404)
op := num.GreaterThan(100)
op := num.LessThan(500)
op := num.GreaterThanOrEqual(200)
op := num.LessThanOrEqual(299)
op := num.Between(200, 299)
```

### BooleanOperators

```go
bool := operators.BooleanOperators{}

op := bool.Equals(true)
op := bool.IsTrue()
op := bool.IsFalse()
```

## Usage with Extractors

Operators are designed to work with extractors in a composition pattern:

```json
{
  "extractorType": "jsonPath",
  "extractorData": {"path": "$.status"},
  "operatorType": "equals",
  "operatorData": {"expected": "success"}
}
```

### Example: Validate Status Code is 2xx

```json
{
  "extractorType": "statusCode",
  "extractorData": {},
  "operatorType": "between",
  "operatorData": {"min": 200, "max": 299}
}
```

### Example: Validate Header Contains Value

```json
{
  "extractorType": "header",
  "extractorData": {"headerName": "Content-Type"},
  "operatorType": "contains",
  "operatorData": {"substring": "application/json"}
}
```

### Example: Validate JSON Field Matches Pattern

```json
{
  "extractorType": "jsonPath",
  "extractorData": {"path": "$.order.id"},
  "operatorType": "regex",
  "operatorData": {"pattern": "^ORD-\\d{6}$"}
}
```

## Adding New Operators

To add a new operator:

1. **Create the operator struct** implementing the `Operator` interface:
```go
type MyCustomOperator struct {
    Config string `json:"config"`
}

func (o MyCustomOperator) Validate(actual interface{}) (bool, error) {
    // Implementation
}

func (o MyCustomOperator) GetType() OperatorType {
    return OperatorTypeMyCustom
}
```

2. **Add the operator type constant**:
```go
const OperatorTypeMyCustom OperatorType = "myCustom"
```

3. **Optionally add factory method** (if type-specific):
```go
func (s StringOperators) MyCustom(config string) Operator {
    return MyCustomOperator{Config: config}
}
```

That's it! The operator is now available throughout the system.

## Design Rationale

This hybrid approach provides:

- **Compile-time type safety** through factory methods
- **Runtime flexibility** through the Operator interface
- **Zero code duplication** for shared operators
- **Clear API** that guides developers to appropriate operators
- **Easy extensibility** without modifying existing code

The design follows the **Strategy Pattern** and aligns with industry best practices from testing libraries like Hamcrest and Chai.js.
