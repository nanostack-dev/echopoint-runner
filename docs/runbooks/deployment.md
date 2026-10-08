# Deployment and publication

There are two independent release paths, defined in [release.yml](../../.github/workflows/release.yml) and [go.yml](../../.github/workflows/go.yml).

Every push to `main` creates the next minor semver tag and runs GoReleaser, including `docs:` commits. [GoReleaser](../../.goreleaser.yml) publishes platform archives and checksums; changelog exclusions do not suppress releases. Verify the release workflow and expected tag/assets after merge.

Go/runtime changes selected by `go.yml` additionally run lint/tests, build the container and dispatch an immutable `main-<short-sha>` image to `nanostack-dev/infra` for development deployment with production promotion requested. Documentation-only changes do not select that path. Verify the infra router's deployment result and a representative flow; a module release is not proof of service rollout. Deployment stacks and runtime health verification belong to [infra](https://github.com/nanostack-dev/infra).

For library consumers, use the actual released version in each consumer checkout:

```sh
go get github.com/nanostack-dev/echopoint-runner@vX.Y.0
go mod tidy
```

Upgrade Echopoint's main module, its `cloudworker` module and the CLI module. Run their affected execution/contract suites. The CLI and Action embed the runner, so CI callers receive the change only after a CLI release and selection of that release. Record the runner version and companion consumer PRs in rollout evidence.
