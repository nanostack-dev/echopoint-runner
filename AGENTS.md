# Echopoint Runner Agent Guide

Go execution engine for webhook events and logical flows, consumed in-process by `../echopoint`.

## Invariants

- Event processing is idempotent — the same event may be delivered more than once.
- `echopoint` owns the accepted API/SSE contract; app/control-plane policy does not belong here.
- Changing an exported progress/execution event shape breaks the consumer: update `echopoint/cmd/http/openapi.yaml` in the same session.
- Optimize JSONPath paths only against a test or a measured need.
