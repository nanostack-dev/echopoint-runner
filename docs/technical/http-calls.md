# HTTP calls

Every execution run through `runner.Run` lists its HTTP calls in `FlowExecutionResult.http_calls`, and enforces the HTTP call limit. Echopoint projects the list into its execution analytics ([Echopoint ADR-0017](https://github.com/nanostack-dev/echopoint/blob/main/docs/adr/0017-execution-analytics-are-a-rebuildable-timescale-projection.md)).

## Recording

`runner.Run` puts one [`httpcall.Recorder`](../../pkg/httpcall/recorder.go) in the execution context. Nested flows (loop bodies, poll bodies, modules) run on that context, so their calls join the same list. Request and SSE nodes call `httpcall.Start` just before sending and `Finish` once the response is read (request) or the stream ends (SSE). Webhook-wait and webhook-expectation reads go to the control plane and are not recorded.

Each entry ([`spi.HTTPCall`](../../pkg/spi/http_call.go)):

| Field | Meaning |
| --- | --- |
| `seq` | Order the call was started in, from 1. |
| `node_id` | The node that sent it. Inside a loop, poll or module this is the node of the nested flow. |
| `method`, `host` | Upper-case method; resolved host with its port. |
| `path_template` | Path of the node's URL *before* resolution: `{{baseUrl}}/users/{{ id }}?x=1` gives `/users/{id}`. Capped at 512 bytes. |
| `status_code` | Absent when no response arrived. |
| `error_class` | `dns`, `connect`, `tls`, `timeout`, `http_4xx` or `http_5xx`; absent for 1xx-3xx. An SSE deadline that elapses before the headers is `timeout`, although the node reports a normal stop. |
| `started_at`, `duration_ms` | SSE duration covers the whole stream. |
| `response_bytes` | Body size read; absent for SSE. |

A call never holds the resolved URL, query string, headers or body, so a secret resolved into a URL cannot leak through it and the list needs no redaction.

## Limit

`runner.WithHTTPCallLimit(n)` refuses the call past `n`: `httpcall.Start` returns a `spi.UserError` with code `HTTP_CALL_LIMIT_EXCEEDED`, the request is not sent and not recorded, and the node fails. A limit of zero or less records without refusing, which is what a control plane that does not send the field gets.

The limit reaches the runner as `max_http_calls_per_execution` on a self-hosted claim (`controlplane.ClaimedJob`), a one-shot `jobrunner.Job` and an ephemeral `Package`.

## Completion

The list travels inside `result` of the completion body. A failed self-hosted run sends its partial `result` too, so the calls of a failed run, the most useful for analysis, reach the control plane; the ephemeral runner already did. A control plane that does not know `http_calls` ignores it.
