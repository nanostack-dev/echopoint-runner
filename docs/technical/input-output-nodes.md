# Input and output nodes

`input` (`spi.KindInput`) and `output` (`spi.KindOutput`) declare a flow's typed caller contract using JSON Schema properties.

## Parse-time gates

`flow.ParseOptions` gates both node kinds generically:

- `AllowInputNodes` (default `false`): rejects any `input` node when false.
- `AllowOutputNodes` (default `false`): rejects any `output` node when false.

Additional parse-time invariants:

- A flow may contain **at most one** `input` node and **at most one** `output` node.
- Inline `loop` and `poll` bodies never allow `input` or `output` nodes (enforced both at parse time in `pkg/flow` and at run time via `spi.ModuleExecutionRequest.AllowContractNodes`, which only `ModuleNode` sets to `true`).
- Every property on an `output` node must declare a non-empty name and a non-nil `value`.

## `input` node

```json
{
  "id": "input",
  "type": "input",
  "data": {
    "required": ["email", "seats"],
    "properties": {
      "email": { "type": "string", "format": "email", "description": "User email" },
      "seats": { "type": "integer" },
      "plan": { "type": "string", "enum": ["starter", "pro"], "default": "pro" },
      "api_secret": { "type": "string", "secret": true }
    }
  }
}
```

- Supported JSON Schema `type` values: `string`, `integer`, `number`, `boolean`, `array`, `object` (omitted or `any` skips type checking).
- Supported `format` checks on strings: `email`, `uuid`, `uri` (`url`), `date` (`YYYY-MM-DD`), `date-time` (RFC3339).
- `enum` checks JSON equality against the allowed list.
- At run time, `InputNode.Execute` reads each property from `ctx.FlowInputs[name]`, falls back to `default` when absent or nil, validates `required` (`data.required` or `property.required`), validates type/format/enum, and publishes outputs as `{{<nodeId>.<name>}}`.
- A property with `"secret": true` registers its value with `redact.LearnOutput`, masking it as `***` across results, progress events, and errors.
- Error codes (`spi.UserError`): `INPUT_REQUIRED`, `INPUT_INVALID`.

## `output` node

```json
{
  "id": "output",
  "type": "output",
  "data": {
    "properties": {
      "summary": { "type": "string", "value": "{{input.email}}:{{input.plan}}" },
      "seats_allocated": { "type": "integer", "value": "{{{input.seats}}}" },
      "token": { "type": "string", "value": "{{login.token}}", "secret": true }
    }
  }
}
```

- `InputSchema()` infers template references from every property's `value`, so `validateFlowReferences` checks that referenced upstream nodes are connected and declare those outputs.
- At run time, `OutputNode.Execute` resolves each property's `value` via `TemplateResolver`, validates `type`/`format`/`enum`, learns any `"secret": true` value via `redact.LearnOutput`, and propagates `redact.Withhold` if any referenced upstream output was withheld.
- Error codes (`spi.UserError`): `OUTPUT_VALUE_MISSING`, `OUTPUT_RESOLUTION_FAILED`, `OUTPUT_INVALID`.

## Auto-export on `module` calls

When a `module` node omits `output_bindings` (`len(data.output_bindings) == 0`):

- At parse time (`flow.ParseFromJSON`), downstream references `{{<moduleNodeId>.<key>}}` are allowed as open references.
- Before execution (`engine.validateModuleGraph`), the engine resolves the child flow and validates that every downstream `{{<moduleNodeId>.<key>}}` matches a property declared on the child flow's `output` node (failing with `MODULE_OUTPUT_NOT_DECLARED` otherwise).
- At run time (`ModuleNode.exportOutputs`), the module exports all properties produced by the child's `output` node (and fails if that `output` node failed), preserving `redact.Withhold` for secret outputs.
- Explicit `output_bindings` retain their existing behavior for backwards compatibility.
