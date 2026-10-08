# Echopoint Runner

Go flow execution engine and self-hosted runner. This checkout works independently; Echopoint owns API/control-plane policy and consumes its exported contracts.

- Before implementation or delivery, read [agent workflow](docs/development/agent-workflow.md) and [testing](docs/development/testing.md).
- Before changing scheduling, node data, progress events or result shapes, read [architecture](docs/technical/architecture.md), [domain context](CONTEXT.md) and the capability docs in [the index](docs/README.md). Coordinate the matching Echopoint API/SSE contract and CLI consumer changes.
- Before local startup, read [setup](docs/development/setup.md); for recurring failures, read [troubleshooting](docs/development/troubleshooting.md).
- Before merging or upgrading a consumer, read [deployment and publication](docs/runbooks/deployment.md). Every push to main, including documentation changes, releases a new minor version. For recovery, read [rollback](docs/runbooks/rollback.md).
- `pkg/core` is an unconsumed rewrite; implement production behavior in the live packages listed in architecture. Error logs are runner faults; `controlplane.IsTransient` and `spi.UserError` failures log at warn. Optimize JSONPath only with a test or measured need.
- Keep instructions and required procedures local; update owning docs in the same PR as the behavior or verified lesson.
