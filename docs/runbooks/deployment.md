# Deployment and publication

There are two independent release paths, defined in [release.yml](../../.github/workflows/release.yml) and [go.yml](../../.github/workflows/go.yml).

Every push to `main` creates the next minor semver tag and runs GoReleaser, including `docs:` commits. [GoReleaser](../../.goreleaser.yml) publishes platform archives and checksums; changelog exclusions do not suppress releases. Verify the release workflow and expected tag/assets after merge.

Go/runtime changes selected by `go.yml` additionally run lint/tests, build the container and dispatch an immutable `main-<short-sha>` image to `nanostack-dev/infra` for development deployment with production promotion requested. Documentation-only changes do not select that path. Verify the infra router's deployment result and a representative flow; a module release is not proof of service rollout. Deployment stacks and runtime health verification belong to [infra](https://github.com/nanostack-dev/infra).

After a merge, confirm the `Go Build and Test` run on that main commit is green and that the runner services log `app.commit` `main-<short-sha>` before a consumer pins the new tag: the Release workflow tags without waiting for the tests, so a red test leaves a released version with no image ([postmortem](../postmortems/2026-11-19-rejected-lease-test-race.md)).

For library consumers, use the actual released version in each consumer checkout:

```sh
go get github.com/nanostack-dev/echopoint-runner@vX.Y.0
go mod tidy
```

[`pkg/flowdsl`](../technical/flow-files.md) is a nested module with its own tags. `release.yml` tags only the root module, so a flowdsl change ships when someone pushes the next `pkg/flowdsl/vX.Y.Z` tag on the merged `main` commit (`git tag --sort=-v:refname -l 'pkg/flowdsl/*' | head -n1` shows the latest). Consumers then run `go get github.com/nanostack-dev/echopoint-runner/pkg/flowdsl@vX.Y.Z` in both Echopoint `go.mod` files and the CLI.

Upgrade Echopoint's main module, its `cloudworker` module and the CLI module. Run their affected execution/contract suites. The CLI and Action embed the runner, so CI callers receive the change only after a CLI release and selection of that release. Record the runner version and companion consumer PRs in rollout evidence.

Security rebuilds pin a patched Go compiler in both go.mod and Docker, and
refresh Alpine runtime packages during the image build. Scan the published image
and verify the runner startup version after rollout. Compiler-only changes do
not require consumers to upgrade the embedded execution library.
