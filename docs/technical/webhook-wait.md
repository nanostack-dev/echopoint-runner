# Webhook wait

A `webhook_wait` node polls the webhook requests captured for its execution with the job token (and the Job's trace context, see [control-plane calls](architecture.md#control-plane-calls)), every 250 ms, until `timeout_ms` (default 30000). It runs in one of two modes.

## First match

Without `expect`, the wait completes on the first captured request that passes the node's assertions. Its outputs are that request's `id`, `method`, `headers`, `query`, `body` and `received_at`. A timeout fails it with `WEBHOOK_WAIT_TIMEOUT` and keeps the five newest requests it saw.

## Expected events

With `expect`, the wait checks a list of named groups. Each group has assertions, a `min` (default 1) and an optional `max`. The node's own assertions are checked on every request a group claims. The only output is `received`, the number of claimed requests.

- **min** counts claimed requests. Each request, oldest first, is claimed by one group: the first group it satisfies that still needs events, else the first group it satisfies.
- **max** counts every request that passes the group's assertions, including requests another group claimed. A `"min": 0, "max": 0` group therefore fails on a matching request even when a broader group claimed it. Its `problem` names that request and the group that claimed it, and `closest` carries that request with `claimed_by`.
- **settle_ms** keeps the wait listening after every group passes, so a later extra event can still break a `max`. It must be shorter than the effective `timeout_ms`; a longer or equal value is rejected when the flow is parsed. If the timeout still fires inside the settle window, every group that had passed fails with `timed out before the <settle_ms>ms settle window ended`.
- **Observation window.** When no group needs an event (every `min` is 0), the wait is met on its first poll. It then listens for `settle_ms` when that is set, else until `timeout_ms`, and passes only if no forbidden event arrived in that window.

A group that can no longer pass, such as one over its `max`, ends the wait at once. Any failed group fails the node with `WEBHOOK_WAIT_EXPECTATIONS_FAILED`, and `expectations[]` reports each group's `received`, `request_ids`, `problem` and `closest`.
