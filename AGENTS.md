# Echopoint Runner Agent Guide

Go execution engine for webhook events and logical flows, consumed in-process by `../echopoint`.

## Invariants

- Event processing is idempotent — the same event may be delivered more than once.
- `echopoint` owns the accepted API/SSE contract; app/control-plane policy does not belong here.
- Changing an exported progress/execution event shape breaks the consumer: update `echopoint/cmd/http/openapi.yaml` in the same session.
- Optimize JSONPath paths only against a test or a measured need.

## Cloud worker

`pkg/cloudworker` plus `cmd/cloudworker` are the Cloud worker: the container that runs one Cloud job per process on Cloudflare Containers. `cloudworker/` holds the Worker in front of it, and `Dockerfile.cloudworker` builds the image.

- It talks to the control plane through `internal/controlplane`, with `Config.JobToken` set. A job token travels alone: the control plane refuses a request that also carries `X-Api-Key`.
- Completion reports the sequence the control plane accepted, never the worker's own counter. A dropped event would otherwise make completion unacceptable and strand the job.
- The container drains on SIGTERM. The platform waits 15 minutes before SIGKILL, and a killed Cloud job is a customer run that never runs again.
- Deploy and Cloudflare specifics: `cloudworker/README.md`.
