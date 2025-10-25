# Assertions

This package provides a flexible, HTTP-agnostic assertion system for validating responses.

## Architecture

The assertion system is built on a **separation of concerns** principle, dividing extraction from validation:

```
┌─────────────────────────────────────────────────────────┐
│                   HTTP Response                         │
│  (statusCode, headers, body)                            │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│              Extraction Layer                           │
│  • JSONPathExtractor    ($.user.name)                   │
│  • XMLPathExtractor     (/response/status)              │
│  • StatusCodeExtractor  (HTTP status)                   │
│  • HeaderExtractor      (Content-Type)                  │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼ (extracted value)
┌─────────────────────────────────────────────────────────┐
│              Assertion Layer                            │
│  • StringAssertion      (equals, contains, regex, etc.) │
│  • NumberAssertion      (>, <, between, etc.)           │
│  • BooleanAssertion     (true/false)                    │
└─────────────────────────────────────────────────────────┘
```

## Extractors

Extractors are responsible for pulling specific values from responses. They are located in `/internal/extractors/`.

### JSONPathExtractor

Extracts values from JSON responses using JSONPath expressions.

```go
extractor := extractors.JSONPathExtractor{
    Path: "$.user.name",
}
```

### XMLPathExtractor

Extracts values from XML responses using XPath expressions.

```go
extractor := extractors.XMLPathExtractor{
    Path: "/response/user/name",
}
```

### StatusCodeExtractor

Extracts the HTTP status code from responses.

```go
extractor := extractors.StatusCodeExtractor{}
```

### HeaderExtractor

Extracts specific HTTP header values.

```go
extractor := extractors.HeaderExtractor{
    HeaderName: "Content-Type",
}
```

## Assertions

Assertions are generic validators that work with any extracted value. They are HTTP-agnostic.

### StringAssertion

Validates string values with various operators.

#### Operators
- `equals` - Exact match
- `notEquals` - Not equal
- `contains` - Contains substring
- `notContains` - Does not contain substring
- `startsWith` - Starts with prefix
- `endsWith` - Ends with suffix
- `regex` - Matches regular expression
- `empty` - String is empty
- `notEmpty` - String is not empty

#### Example
```go
assertion := assertions.StringAssertion{
    Operator: assertions.StringOperatorEquals,
    Expected: "success",
}
```

### NumberAssertion

Validates numeric values with comparison operators.

#### Operators
- `equals` - Exact match
- `notEquals` - Not equal
- `greaterThan` - Greater than
- `greaterThanOrEqual` - Greater than or equal
- `lessThan` - Less than
- `lessThanOrEqual` - Less than or equal
- `between` - Within range (uses Min and Max fields)

#### Example
```go
assertion := assertions.NumberAssertion{
    Operator: assertions.NumberOperatorBetween,
    Min:      200,
    Max:      299,
}
```

### BooleanAssertion

Validates boolean values.

#### Example
```go
assertion := assertions.BooleanAssertion{
    Expected: true,
}
```

## Composition Pattern

Extractors and assertions are composed together using the `CompositeAssertion` struct in request nodes.

### Example: Validate JSON field equals a value

```json
{
  "extractorType": "jsonPath",
  "extractorData": {"path": "$.user.name"},
  "assertionType": "string",
  "assertionData": {"operator": "equals", "expected": "John Doe"}
}
```

This:
1. Extracts the value at `$.user.name` using JSONPathExtractor
2. Validates it equals "John Doe" using StringAssertion

### Example: Validate status code is 2xx

```json
{
  "extractorType": "statusCode",
  "extractorData": {},
  "assertionType": "number",
  "assertionData": {"operator": "between", "min": 200, "max": 299}
}
```

### Example: Validate header contains value

```json
{
  "extractorType": "header",
  "extractorData": {"headerName": "Content-Type"},
  "assertionType": "string",
  "assertionData": {"operator": "contains", "expected": "application/json"}
}
```

## Benefits

1. **HTTP-Agnostic**: Assertions work with any data source, not just HTTP
2. **Reusable**: StringAssertion can validate JSONPath results, header values, or any string
3. **Composable**: Mix and match extractors with assertions
4. **Testable**: Extract and assert logic can be tested independently
5. **Extensible**: Add new extractors (e.g., GraphQLExtractor) or assertions (e.g., JSONSchemaAssertion) easily
