# Cloud worker on Cloudflare Containers

The production Cloud worker. `echopoint` assigns a Cloud job to it and never
executes the flow itself. The fake worker in echopoint's component tests drives
`pkg/cloudworker` directly, so the default test suite needs no Cloudflare.

## Parts

| Part | Path | Role |
|---|---|---|
| Worker | `src/index.ts` | Authenticates assign, picks the tier class, forwards assign |
| Container server | `../pkg/cloudworker/server.go` | Runs up to `CLOUD_WORKER_MAX_JOBS` jobs, reports load on `GET /status`, drains on SIGTERM |
| Container entrypoint | `../cmd/cloudworker/main.go` | Serves assign, runs one runner process per job |
| Job run | `../pkg/cloudworker/run.go` | Fetches the payload, runs the flow, sends events, heartbeat, and the result |
| Image | `../Dockerfile.cloudworker` | Container image, build context is the repository root |

## Assign path

1. The control plane POSTs `{job_id, job_token, tier}` with
   `Authorization: Bearer <ASSIGN_SECRET>`. No flow snapshot, no runner inputs,
   no secrets from the flow.
2. The Worker picks the container class for the tier and forwards assign to a
   container of that tier. An unknown tier is a `400`.
3. The container answers `202`, or `429` when it is full or draining. The Worker
   then tries the next container of the tier. A tier with no room answers `503`,
   which is a capacity refusal rather than a failed job.
4. The runner process fetches `/runner/jobs/{id}/payload` with `X-Job-Token`,
   runs the flow, heartbeats every 10 seconds, and posts events and complete.

## Tiers

The Cloudflare instance type belongs to the container class, and no API sets it
per instance. A tier is therefore a class, not a Durable Object name.

| Tier | Class | Instance type | Memory | Containers | Jobs per container |
|---|---|---|---|---|---|
| `lite` | `CloudWorkerLite` | `lite` | 256 MiB | 2 | 2 |
| `standard` | `CloudWorkerStandard` | `basic` | 1 GiB | 2 | 4 |

`max_instances` counts running containers, and a start above it fails rather
than waits. `CLOUD_WORKER_LITE_CONTAINERS` and `CLOUD_WORKER_STANDARD_CONTAINERS`
must match those numbers: they are how many containers the Worker addresses.

Memory is the ceiling on jobs per container. Response bodies are read whole, and
one container out of memory restarts every job on that container. Raise
`CLOUD_WORKER_MAX_JOBS` only with the instance memory raised beside it.

## Idle sleep

`sleepAfter` is 1 minute. On expiry the Worker asks the container `GET /status`.
A container that holds a job renews the activity timeout. An unreachable
container counts as busy for two probes, then the Worker stops it.

## Rollouts

A rollout sends SIGTERM and waits up to 15 minutes. The container refuses new
assigns, finishes the jobs it holds, and exits.

## Deploy

```bash
pnpm install && pnpm typecheck
```

```bash
pnpm wrangler secret put ASSIGN_SECRET
```

```bash
pnpm deploy
```

Set `vars.ECHOPOINT_API_BASE_URL` in `wrangler.jsonc` to the control plane base
URL of the target environment. The container reads it as an environment
variable, and reaches the control plane over the public internet.
