# HTTP calls

Every node result lists the HTTP calls the node sent in `http_calls`, and every execution run through `runner.Run` enforces the HTTP call limit. The planned Echopoint projection uses these calls for execution analytics ([Echopoint ADR-0017](https://github.com/nanostack-dev/echopoint/blob/main/docs/adr/0017-execution-analytics-are-a-rebuildable-timescale-projection.md)).

## Recording

Request and SSE nodes record at the HTTP transport boundary, so each followed redirect counts as another call. Calls finish when their response body closes; response sizes include bytes read before a read error. Redirects keep the sending node's original path template and use the destination host and method. Cloud still refuses redirects. Webhook-wait and webhook-expectation reads go to the control plane and are not recorded.

The engine runs each top-level node on a context carrying its own `httpcall.Collector` and, once the node returns, sets the collected calls on its result before the `NodeFinished` event. Loop, poll and module bodies run on that node's context, so their calls land on the top-level node: a loop's result holds every iteration's calls, a poll's every attempt's. Retry middleware runs inside the same context, so each attempt is a call. The calls therefore reach the control plane with the node result, through progress events and the completion's `execution_results`.

## Transport retries

A request node sends a request up to three times when its connection fails before any response arrives, waiting 100 ms, then 200 ms, inside the node's timeout ([`request_retry.go`](../../pkg/node/request_retry.go)). Each attempt is its own call, so a recovered failure still shows in `transport_errors`.

| Failure | Retried for |
| --- | --- |
| Dial failure, such as a refused connection: no request left the runner | Every method |
| Connection reset or closed (`EOF`) after the request was written | `GET`, `HEAD`, `OPTIONS`, `TRACE`, `PUT`, `DELETE`, and any request with an `Idempotency-Key` or `X-Idempotency-Key` header |
| Timeout, DNS, TLS, cancellation, HTTP call limit, any HTTP status | Never |

A `POST` or `PATCH` without an idempotency key is not resent after a reset: the target may already have run it, and a second send could create a duplicate. HTTP statuses and assertion failures are left to the flow. The motivating failures were Cloudflare connection resets that failed about 1% of the dev gate-flow runs in November 2026.

### SSE reconnects

An SSE node opens at most three connections ([`sse_reconnect.go`](../../pkg/node/sse_reconnect.go)). It reconnects after a connect failure, and after a stream the transport cuts (a reset or an unexpected EOF) before `max_events`, `completion_event` or an assertion stopped it. The same table decides: a `GET` stream reconnects, a `POST` stream without an idempotency key does not. The waits are 100 ms, then 200 ms, and every connection shares the node's `timeout_ms`; the `retry:` field is ignored. A clean end of stream, an HTTP status, an assertion failure and the deadline never reconnect. Each connection is its own call.

What happens to the events already collected depends on whether the stream gave event ids:

| Stream | Reconnect | Events after the reconnect |
| --- | --- | --- |
| Gave an `id:` | Resume: sends `Last-Event-ID` with the last id | Kept; an event whose id was already collected is dropped, so a server that ignores `Last-Event-ID` and replays is not counted twice |
| Gave no id | Restart | Discarded with their assertion results; the node collects the new connection's events only |

Restart fits servers that replay the stream on every connect, such as Echopoint's execution stream: it sends no ids, ignores `Last-Event-ID` and replays every stored event, then the live ones. On a live-only server without ids, a restart loses the events sent before the cut. A restart never runs when an event assertion already failed under `stop_on_assertion_failure: false`, because it would erase that failure; the node fails with `SSE stream read error` as before. After the last attempt the node fails with that error and the last connection's events.

The motivating failure was a gate run on 2026-11-19 whose SSE nodes failed on `connection reset by peer` from Cloudflare while the API logged the streams as 200.

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

## Response size

A request node reads at most 10 MiB of response body (`node.DefaultMaxResponseBytes`). A larger body is not kept: the node fails with `RESPONSE_TOO_LARGE`, naming the host and the limit. A `Content-Length` above the limit fails before the body is read; otherwise the read stops one byte past the limit. `runner.WithMaxResponseBytes(n)` changes the limit for a run; the self-hosted runner and ephemeral mode read it from `ECHOPOINT_MAX_RESPONSE_BYTES` (bytes, default 10485760).

Assertions and outputs evaluate against the whole body, but a request result reports at most 1 MiB of it (`node.ReportedResponseBodyBytes`). Past that, `response_body` holds the first 1 MiB, `response_body_parsed` is left out and `response_body_truncated` is `true`, in progress events and the completion alike. The full size remains in the call's `response_bytes`. Secret masking runs on the reported body, so only a secret cut by the 1 MiB boundary can leave its first characters unmasked.

## Response memory

A request node holds only what its assertions and outputs need, and only while they run:

- A node whose assertions and outputs use only `status_code` and `header` extractors keeps the first 1 MiB of the body, the part its result reports, and counts the rest without storing it. The size limit still applies to the whole body.
- Any other extractor reads the body, so the node keeps all of it. A body above 1 MiB is parsed (JSON tree or text) only when an extractor first asks for it; a smaller one is parsed at once, because its result reports the parsed form.
- Once the engine has run the node's assertions, outputs and secret-output learning, it releases the response ([`ResponseReleaser`](../../pkg/node/assertion_eval.go)). Each attempt of a retried node releases its own. From then on the result keeps only its reported body, so a finished node holds at most 1 MiB until the flow ends.

### Buffered response budget

A response budget ([`node.ResponseBudget`](../../pkg/node/response_budget.go)) bounds the bodies held at once by every request node of every flow sharing it, so parallel nodes and parallel flows cannot multiply memory without bound. Before reading a body, a node reserves two budget bytes per byte it keeps: one for the bytes, one for the parsed form built from them. It holds the reservation until its response is released. A body with `Content-Length` reserves it at once; one without reserves 64 KiB, then, if it is longer, what the node may keep, and gives back the unused part when the read ends. A body costing more than the whole budget waits for all of it and is read alone.

A node that cannot reserve waits. The wait counts against the node's own request timeout; when the timeout elapses first, the node fails with `REQUEST_TIMEOUT` ("timed out waiting for runner memory to read its response"). The budget counts response bodies only: outputs extracted from a body, reported results and progress events add to it.

| Runner | Budget |
| --- | --- |
| Self-hosted (`serve`) | `ECHOPOINT_MAX_BUFFERED_RESPONSE_BYTES` (bytes, default 134217728, 128 MiB), shared by all `ECHOPOINT_MAX_PARALLEL_FLOWS` jobs |
| Ephemeral mode | `ECHOPOINT_MAX_BUFFERED_RESPONSE_BYTES`, same default, for its one execution |
| One-shot job runner (`pkg/jobrunner`) | 128 MiB (`config.DefaultMaxBufferedResponseBytes`) |
| Library callers of `runner.Run` | none unless `runner.WithResponseBudget` passes one |

The default holds six fully read 10 MiB bodies, or 64 responses whose nodes only check status and headers. Go's collector lets the heap grow to about twice its live size, so a runner that only reads large responses peaks near twice the budget plus its own working set. Give a container at least four times the budget, or lower the budget for smaller containers.

`BenchmarkConcurrentLargeResponses` in [`internal/runtime`](../../internal/runtime/response_memory_test.go) measures this end to end (3 flows of 4 parallel 10 MiB GETs through the runtime, redaction and progress reporting), and `TestConcurrentLargeResponsesStayWithinTheMemoryTarget` keeps the peak heap under 300 MiB.

## Compatibility

Echopoint decodes node results into typed structs with `encoding/json`, so a control plane that does not know `http_calls` ignores it.

## Self-hosted request pacing

Set `ECHOPOINT_MS_BETWEEN_REQUESTS` to a non-negative integer in milliseconds
(for example, `100`) to space outbound flow request starts in a self-hosted
runner. Unset or `0` disables pacing. One pacer is shared by every concurrent
job in the process, including request/SSE nodes, redirects, retries and nested
flows. Separate runner replicas have independent pacers. The first request is
immediate; an idle runner does not accumulate burst capacity.

Waiting honors the request's cancellation and deadline. Calls are recorded only
after pacing, so queue time is included in node duration but not HTTP call
duration. Control-plane polling, heartbeats, progress, completion and webhook
reads are unaffected. This setting applies to the self-hosted runtime; embedded
library consumers keep their existing behavior.
