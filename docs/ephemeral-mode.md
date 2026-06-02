# Ephemeral package mode

`echopoint-runner` has two modes:

- **`serve`** (default, also the bare `echopoint-runner` invocation): the long-lived self-hosted
  runner that claims queued jobs from the control plane. Unchanged.
- **`ephemeral`**: a one-shot mode that executes a single execution package provided on stdin or a
  file and writes a result payload. It does **not** claim jobs, send heartbeats, post progress
  events, or call any completion endpoint, and it does **not** require `ECHOPOINT_RUNNER_API_KEY`.

Ephemeral mode is used by `echopoint flows run` (and the GitHub Action) to run a flow locally on a
CI worker. The CLI launches the flow against the server with `runner_type=ephemeral`, builds this
package from the launched execution's `flow_snapshot` / `runner_inputs` / `referenced_flows` fields,
runs it through this mode, and publishes the result via
`POST /runner/ephemeral/executions/{executionId}/complete`.

## Usage

```bash
# From files
echopoint-runner ephemeral --input package.json --output result.json

# From stdin to stdout (logs go to stderr; only the result JSON goes to stdout)
echopoint-runner ephemeral --input - --output -
```

Both `--input` and `--output` default to `-` (stdin/stdout).

## Package input

The package is assembled by the CLI from the launched execution's runnable fields
(`flow_snapshot` → `flow_definition`, `runner_inputs` → `inputs`, plus `referenced_flows`). The
runner's stdin contract is unchanged:

```json
{
  "execution_id": "exec_...",
  "flow_id": "flow_...",
  "flow_definition": { },
  "inputs": { "BASE_URL": "https://api.example.com", "API_TOKEN": "<resolved secret>" },
  "referenced_flows": { "flow_child": { "flow_definition": {}, "input_overrides": {} } }
}
```

`inputs` are the resolved execution inputs/env and may contain secrets. The flow is executed with
the existing engine; referenced/module flows are resolved from `referenced_flows`.

## Result output

The result is the runner's stdout contract; the CLI forwards it as the
`POST /runner/ephemeral/executions/{executionId}/complete` request body
(`EphemeralCompletionRequest`):

```json
{
  "status": "completed",
  "started_at": "2026-06-01T12:00:00Z",
  "completed_at": "2026-06-01T12:00:05Z",
  "duration_ms": 5000,
  "result": { "execution_results": {}, "final_outputs": {}, "success": true },
  "error_code": null,
  "error_message": null
}
```

A flow that fails normally still produces a `status: "failed"` result with error fields populated —
the result JSON is the authoritative signal for the CLI.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The flow completed successfully. |
| `1` | The flow ran but produced a failed result. A result JSON is still written. |
| `3` | API / runner / contract error: invalid or unparseable package, I/O failure, or engine init error (occurs before a normal flow result). |

The CLI maps these as: exit 0 → publish the completed result; exit 1 → publish the failed result
(the stdout result JSON is authoritative); exit 3 → treat as a hard error.

## Secret hygiene

- Raw `inputs`/env values are never logged — at most the sorted key names are logged at info level.
- All log output goes to **stderr**; in stdout mode only the result JSON is written to stdout.
- Ephemeral mode constructs no control-plane claim/complete/progress clients, so no runner API key
  or progress traffic is involved.
