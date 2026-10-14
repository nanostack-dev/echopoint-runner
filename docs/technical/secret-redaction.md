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

## Capability declaration

A runner older than secret output support ignores the flag and reports the value in clear. So every self-hosted claim (`POST /runner/jobs/next`) declares `"capabilities": ["secret_outputs"]` ([`controlplane.Capabilities`](../../internal/controlplane/client.go)). A caller that claims a job itself, such as the CLI's ephemeral run, declares [`jobrunner.Capabilities()`](../../pkg/jobrunner/capabilities.go). The control plane fails a job whose flow or referenced flows declare a secret output when its claim lacks the capability (Echopoint ADR-0019).

## Learned values

A secret output's value is learned leaf by leaf. A leaf of at least 6 characters (`redact.MinLearnedLength`) is masked by value, up to 500 distinct values per run (`redact.MaxLearnedValues`); a value learned twice counts once. Any other leaf is too short to mask by value without masking that text everywhere in the run, or would pass the cap, so the output is **withheld** instead. This fails closed:

- The output is masked by name: in its node's `outputs`, in `final_outputs` (`mint.pin`), in the `inputs` of every node that references it (`mint.pin`, `mint.pin.0`), in a module's `child_final_outputs`, and in a module output bound to it.
- The node that extracted it is reported with only the fields that cannot carry it: `node_id`, `display_name`, `node_type`, `run_when`, `inputs`, `outputs`, `error_code`, `error_message`, `skip_reason`, `missing_inputs`, `executed_at`, `duration_ms`, `response_status_code`, `http_calls` and `assertion_results` with `actual` and `expected` masked. Its request, response and child detail are dropped.

A body flagged secret therefore masks its long leaves wherever they appear, keeps short text such as `ok` or `true` readable in other nodes, and drops the response that produced it.

The engine adds a secret output's value before it reports the node that extracted it ([`learnSecretOutputs`](../../pkg/engine/execution.go)). The run's redactor travels in the node context, so module, loop and poll bodies add to the same set. A node that fails before reporting a secret output, because another output or an assertion template erred, still has the value extracted from its response and added.

## Logs

Log lines are written outside the reporting boundary, so no producer logs a resolved value. A node failure message can still quote one: a failed assertion reports `expected=… actual=…`. The engine's `Node execution failed` line, the loop's `Loop iteration failed; continuing` line and the poll's `Poll body execution failed` and `Poll exit condition not yet met` lines therefore mask that message through the run's redactor with [`redact.ErrorMessage`](../../pkg/redact/context.go), which the node context carries. A value masked by name only, rather than by value, is not masked in logs.

## Limitations

- Masking matches text. A short secret input such as `1` masks every occurrence of that text in the run's reports. A withheld secret output is masked only by name, so a consumer that embeds it in its request URL, headers or body, or a response that echoes it, still reports it. Only flag outputs that hold credentials or tokens. The fields the runner writes itself (`node_id`, `node_type`, `run_when`, `error_code`, `executed_at`) are never masked, so the report still decodes. A secret found only in the base64 text of a `response_body`, not in its bytes, replaces the whole body.
- Inside JSON structures only string leaves are scanned. A numeric secret is masked in text such as a body or an error, but not as a JSON number.
- Only the value's exact text and its JSON-escaped forms are matched. A server that echoes it URL-encoded, base64-encoded or split, such as a JWT's segments, is not caught.
- A value is masked from the moment it becomes secret. A secret output value that already appeared in an earlier report, for example one a server returned before the flow extracted it, is not masked retroactively.
- Results that were stored before a value was declared secret keep the value. Removing it means purging those results in the control plane, and the credential should be rotated.

Tests: [`pkg/redact`](../../pkg/redact/redact_test.go), [secret input redaction](../../pkg/runner/redaction_test.go), [secret output redaction](../../pkg/runner/secret_output_test.go) and [log redaction](../../pkg/runner/log_redaction_test.go).
