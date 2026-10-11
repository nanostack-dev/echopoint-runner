# Template references

Node `data` values reference flow inputs and upstream node outputs through templates. [`TemplateResolver`](../../pkg/node/template_resolver.go) resolves them; [flow validation](../../pkg/flow/validation.go) and the [engine's input assembly](../../pkg/engine/engine.go) apply the same lookup rules. Generated `{{$name}}` values are described in [dynamic template variables](../dynamic-template-variables-reference.md).

## Forms

| Template | Result |
| --- | --- |
| `{{ref}}` inside text, such as `/accounts/{{ref}}` | The value formatted as text. |
| `{{{ref}}}` as the whole value | The value with its type kept: an array stays an array, an object stays an object. |
| `{{ref}}` as the whole loop `items` value | The same as `{{{ref}}}`, so `"items": "{{list_users.ids}}"` iterates the array. |

An unresolved reference is left unchanged: `{{ref}}` stays in the text and `{{{ref}}}` returns the original string. A loop whose `items` does not resolve to an array fails with `loop items must resolve to a list`.

## References

- `node_id.output` reads a declared output of an upstream node.
- `node_id.body`, `node_id.status` and `node_id.headers` read the response of an upstream request node without a declared output; see [response outputs](#response-outputs).
- `name` reads a flow input. A loop body receives the current item and index as the flow inputs `item` and `index`, or the names set by `item_var` and `index_var`.
- A flow input whose name contains a dot, such as the launch-injected `webhook.url`, is read as that input when no node with the first segment's ID has run.

## Paths into values

A reference may continue past an output or input into its value. Object fields are selected by name and array elements by zero-based index:

| Reference | Value read |
| --- | --- |
| `{{item.id}}` | Field `id` of the current loop item. |
| `{{{item.tags}}}` | The array in field `tags`, with its type kept. |
| `{{list_accounts.accounts.0.id}}` | Field `id` of the first element of the `accounts` output of `list_accounts`. |

The lookup rules are:

1. A key equal to the whole reference wins, so an existing dotted key keeps its value.
2. Otherwise the longest dotted prefix that is a key is used. Keys may contain dots, such as `list_accounts.accounts`, so the longest match is taken before walking.
3. The remaining segments walk into that value. A missing field, a non-numeric or out-of-range index, or a scalar with segments left makes the reference unresolved.
4. The namespace is chosen once. A reference rooted at a step that has published outputs reads only that step's outputs, never a flow input of the same name; otherwise a flow input owns the path, even when a field is missing, so the miss is reported as a missing input rather than blamed on a skipped step.

Walking works on any slice, array or string-keyed map, so Go callers passing `[]string` or a webhook wait's `map[string]string` headers get the same access as decoded JSON. Assertion expected values and webhook expectations resolve references with the same rules.

A path segment cannot contain a dot, so a field whose own name contains one, such as `pick.account_id` in an element of a loop's `results`, cannot be selected by a path; reference the whole element with `{{{loop.results.0}}}` instead.

Flow validation accepts a path when its root is a known flow input or a declared upstream output; it cannot check the fields inside the value, which are only known at run time. A child module is validated with every input it will receive: those inherited from its parent, its bindings and its flow's overrides. `{{$name}}` dynamic variables are never walked.

## Response outputs

Every request node exposes its response as three outputs nobody declares, so `{{create_customer.body.data.id}}`, `{{create_customer.status}}` and `{{create_customer.headers.Location}}` work without an extractor:

| Output | Value |
| --- | --- |
| `body` | The parsed JSON value when the response `Content-Type` contains `application/json`, otherwise the body text, following [`parseResponseBody`](../../pkg/node/request_execution.go). A body that is not valid UTF-8 text, such as an image, is not exposed. XML stays text: it is not converted to a tree, and the `xml_path` extractor is not implemented yet ([`xml_path_extractor.go`](../../pkg/extractors/xml_path_extractor.go)), so an XML field cannot be extracted on its own. |
| `status` | The HTTP status code as a number. |
| `headers` | An object of the first value of each response header under its canonical name, such as `Location` or `Set-Cookie`, which is what a `header` output reads. |

A declared output with the same name replaces the response output. Declared outputs remain the way to name a value, extract from text, project with a filter such as `$[*].id`, bind a module output and keep a value in the result.

Response outputs live only in run memory, read by later steps' templates and assertions. They are not added to the node result's `outputs`, the flow's `final_outputs`, a loop's `results` or a module's output bindings, because the result already reports the response once. When a later step references one, the referenced value is stored where that step stores its own inputs and request, and passes through the same secret masking as the response itself. A request that received no response, such as one that timed out, exposes none; a reference to it is then a missing input. A poll condition reads its body's final outputs, so it needs declared outputs.
