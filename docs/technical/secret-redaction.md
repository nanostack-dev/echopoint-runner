# Secret redaction

The runner masks secret values in everything it reports: node results, the flow result, `node.finished` and `flow.finished` progress events, and returned errors. The engine itself keeps working with the real values, so downstream nodes still send them. Masking happens once, at the [`runner.Run`](../../pkg/runner/runner.go) boundary, through [`pkg/redact`](../../pkg/redact/redact.go).

## What is secret

| Source | Declared by | Masked from |
| --- | --- | --- |
| Secret input | `secret_input_keys` on the job, ephemeral package or referenced flow (`runner.WithSecretInputKeys`) | the start of the run |
| Secret output | `"secret": true` on a node output | the moment the node extracts it |

```json
{"name": "jwt", "secret": true, "extractor": {"type": "json_path", "path": "$.jwt"}}
```

Redaction works by value, not by field. Every string in a report that contains a secret value has that value replaced by `***`, including a base64 `response_body` (decoded, masked and re-encoded) and the JSON-escaped forms of the value. As a result:

- A secret output is masked in the response that produced it, in its node's `outputs`, in `final_outputs`, in the `inputs` and request headers of every node that references it, and in errors.
- A module output bound to a child's secret output (`output_bindings: {"token": "mint.jwt"}`) is masked in the module result, its `child_final_outputs` and its consumers, without its own flag: it carries the same value.
- A map or array output marks each of its leaves secret.

The engine adds a secret output's value before it reports the node that extracted it ([`learnSecretOutputs`](../../pkg/engine/execution.go)). The run's redactor travels in the node context, so module, loop and poll bodies add to the same set. A node that fails before reporting a secret output, because another output or an assertion template erred, still has the value extracted from its response and added.

## Limitations

- Masking matches text. A short or common secret value such as `1` or `true` masks every occurrence of that text in the run's reports. Only flag outputs that hold credentials or tokens. The fields the runner writes itself (`node_id`, `node_type`, `run_when`, `error_code`, `executed_at`) are never masked, so the report still decodes. A secret found only in the base64 text of a `response_body`, not in its bytes, replaces the whole body.
- Inside JSON structures only string leaves are scanned. A numeric secret is masked in text such as a body or an error, but not as a JSON number.
- Only the value's exact text and its JSON-escaped forms are matched. A server that echoes it URL-encoded, base64-encoded or split, such as a JWT's segments, is not caught.
- A value is masked from the moment it becomes secret. A secret output value that already appeared in an earlier report, for example one a server returned before the flow extracted it, is not masked retroactively.
- Results that were stored before a value was declared secret keep the value. Removing it means purging those results in the control plane, and the credential should be rotated.

Tests: [`pkg/redact`](../../pkg/redact/redact_test.go), [secret input redaction](../../pkg/runner/redaction_test.go) and [secret output redaction](../../pkg/runner/secret_output_test.go).
