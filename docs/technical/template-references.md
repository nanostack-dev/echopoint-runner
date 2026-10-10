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

A path segment cannot contain a dot, so a field whose own name contains one, such as `pick.account_id` in an element of a loop's `results`, cannot be selected by a path; reference the whole element with `{{{loop.results.0}}}` instead.

Flow validation accepts a path when its root is a known flow input or a declared upstream output; it cannot check the fields inside the value, which are only known at run time. `{{$name}}` dynamic variables are never walked.
