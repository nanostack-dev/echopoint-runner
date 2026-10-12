# HTTP calls

Every node result lists the HTTP calls the node sent in `http_calls`, and every execution run through `runner.Run` enforces the HTTP call limit. The planned Echopoint projection uses these calls for execution analytics ([Echopoint ADR-0017](https://github.com/nanostack-dev/echopoint/blob/main/docs/adr/0017-execution-analytics-are-a-rebuildable-timescale-projection.md)).

## Recording

Request and SSE nodes record at the HTTP transport boundary, so each followed redirect counts as another call. Calls finish when their response body closes; response sizes include bytes read before a read error. Redirects keep the sending node's original path template and use the destination host and method. Cloud still refuses redirects. Webhook-wait and webhook-expectation reads go to the control plane and are not recorded.

The engine runs each top-level node on a context carrying its own `httpcall.Collector` and, once the node returns, sets the collected calls on its result before the `NodeFinished` event. Loop, poll and module bodies run on that node's context, so their calls land on the top-level node: a loop's result holds every iteration's calls, a poll's every attempt's. Retry middleware runs inside the same context, so each attempt is a call. The calls therefore reach the control plane with the node result, through progress events and the completion's `execution_results`.

## Format

Calls are grouped by sending node, method, host and path template ([`spi.HTTPCallGroup`](../../pkg/spi/http_call.go)). The fixed fields are stored once; the varying ones are parallel arrays where index `i` is call `i`, in start order.

```json
"fetch_each": {
  "node_type": "loop", "iterations": 4,
  "http_calls": [{
    "node_id": "get_item", "method": "GET", "host": "api.acme.com", "path_template": "/items/{item}",
    "started_at": "2026-10-11T14:00:03.120Z",
    "offsets_ms":     [0,   72,  140, 215],
    "durations_ms":   [70,  65,  72,  30000],
    "status_codes":   [200, 404, 200, 0],
    "response_bytes": [512, 40,  511, 0],
    "transport_errors": { "3": "timeout" }
  }]
}
```

| Field | Meaning |
| --- | --- |
| `node_id` | The node that sent the calls. Inside a body it is the body's node, not the node holding the group. |
| `host` | Resolved host, with its port when the URL names one; casing is preserved so secret-value redaction can match. |
| `path_template` | Path of the node's URL *before* resolution: `{{baseUrl}}/users/{{ id }}?x=1` gives `/users/{id}`. |
| `started_at`, `offsets_ms` | Start of the first call, and each call's start in milliseconds after it. |
| `durations_ms` | An SSE call lasts as long as the stream. |
| `status_codes` | `0` when no response arrived. A 4xx or 5xx is a failure the status already says, so it has no class. |
| `transport_errors` | Index to `dns`, `connect`, `tls` or `timeout`, only for calls without a response. An SSE deadline elapsing before the headers is `timeout`, although the node reports a normal stop. |
| `response_bytes` | Body size read; absent for SSE. |

A group never holds the resolved URL, query string, headers or body.

## Limit

`runner.WithHTTPCallLimit(n)` puts an `httpcall.Limiter` on the run: it counts every call of the execution, whichever node or nested body sends it. The call past `n` is not sent and not recorded; `httpcall.Start` returns a `spi.UserError` with code `HTTP_CALL_LIMIT_EXCEEDED` and the node fails. Zero or less never refuses, which is what a control plane that does not send the field gets.

The limit reaches the runner as `max_http_calls_per_execution` on a self-hosted claim (`controlplane.ClaimedJob`), a one-shot `jobrunner.Job` and an ephemeral `Package`.

## Compatibility

Echopoint decodes node results into typed structs with `encoding/json`, so a control plane that does not know `http_calls` ignores it.
