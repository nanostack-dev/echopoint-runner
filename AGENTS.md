# Echopoint Runner Agent Guide

Go execution engine for webhook events and logical flows, consumed in-process by `../echopoint`.

## Invariants

- Event processing is idempotent — the same event may be delivered more than once.
- `echopoint` owns the accepted API/SSE contract; app/control-plane policy does not belong here.
- Changing an exported progress/execution event shape breaks the consumer: update `echopoint/cmd/http/openapi.yaml` in the same session.
- Optimize JSONPath paths only against a test or a measured need.
- Error level is for runner faults; transient control-plane failures (`controlplane.IsTransient`) and flow-author failures (`spi.UserError`) log at warn.
- Avoid comments — name variables and functions clearly instead. Comment only a genuinely complex algorithm.

## Live Engine

- Production code lives in `pkg/engine`, `pkg/node`, `pkg/flow`, plus `pkg/spi`, `pkg/runner`, `pkg/dynamicvars`, `pkg/executionevents`, `pkg/jobrunner` — the packages `echopoint`, `echopoint/cloudworker`, and `echopoint-cli` import.
- `pkg/core` is a greenfield rewrite that no consumer imports (see `pkg/core/README.md`); change production behaviour in the live packages.
- Node `data` JSON keys are snake_case and must match the `*NodeData` schemas in `../echopoint/cmd/http/openapi.yaml`; a camelCase key is silently dropped (no `DisallowUnknownFields`).

## Commands

- Test: `go test ./...` (CI: `gotestsum -- ./...`). Lint: `golangci-lint run`.

## Releases

- `.github/workflows/release.yml` runs on every push to `main`, whatever the commit type, and always bumps the minor of the highest semver tag (`v0.67.0` -> `v0.68.0`). It never cuts a patch, never reads commit messages, and cuts no major.
- `docs:`/`test:`/`chore:` merges still cut a version; goreleaser only omits them from the changelog.
- Merge order sets version order; each merged PR is one minor.

## After A Release

- Bump the runner in `../echopoint/go.mod`, `../echopoint/cloudworker/go.mod`, and `../echopoint-cli/go.mod`: `go get github.com/nanostack-dev/echopoint-runner@vX.Y.0 && go mod tidy`.
- The CLI and GitHub Action embed the runner; CI flow suites keep running the old runner until the CLI releases.
