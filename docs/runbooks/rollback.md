# Rollback

For a regression in an embedded consumer, restore its last verified runner module version, run `go mod tidy`, execute that consumer's regression suite, and release/redeploy the consumer. A rollback in this repository alone does not change a pinned consumer.

For a self-hosted binary, restore the prior verified archive and its configuration; for the managed container, request the previously verified immutable image through the infra deployment runbook. Verify job claiming, progress and a representative final result against the matching control-plane contract. Account for in-flight executions before replacing a long-lived process; its shutdown grace period is configured in `internal/config`.

Published tags/assets remain immutable history. To fix the release source, revert the offending change through a PR; its merge releases another minor. Preserve release, image and consumer version evidence until recovery is verified.
