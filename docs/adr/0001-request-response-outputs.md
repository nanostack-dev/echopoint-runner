# 0001. Expose request responses as implicit outputs

Status: accepted (2026-10-10)

## Context

A flow author who wanted the ID a request created had to declare an output with a JSONPath extractor before any later step could reference it. Once [template references](../technical/template-references.md) could walk dotted paths into a value, the response itself could be the value walked. Webhook Wait already exposes `body`, `headers` and `query` the same way.

## Decision

Every request node exposes `body`, `status` and `headers` without a declaration. `body` follows the existing parse rule: JSON when the `Content-Type` is JSON, text otherwise; XML is not converted to a tree, because its shape is ambiguous and XPath outputs already cover it, and bytes that are not UTF-8 text are not exposed. A declared output of the same name replaces the implicit one.

Declared outputs stay, permanently and without migration. They name values, extract from XML and text, project with filters, cross module boundaries and are stored in results; paths are a shortcut over values that are already structured.

The implicit outputs live in run memory only. The node result already reports the response; copying it into `outputs`, `final_outputs` and every loop iteration's `results` would store each body again.

## Alternatives

- Default extractors returned by `GetOutputs`, as the loop node does for `results` and `count`. Rejected: extracted outputs are stored in the result and the flow's final outputs, which would duplicate every response body.
- Declared outputs only. Rejected: the common case, reading one field of a JSON response, needed a separate declaration step.

## Consequences

- References such as `{{create_customer.body.data.id}}` validate and resolve without an extractor.
- Module output bindings, loop `results` and poll conditions read final outputs, so they still need declared outputs.
- A referenced value is stored where the consuming step stores its inputs and request, and is masked there by the same secret redaction as the response.
