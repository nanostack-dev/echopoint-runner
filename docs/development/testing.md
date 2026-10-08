# Testing

Run from the repository root:

```sh
go test ./...
golangci-lint run --path-mode=abs
```

The [CI workflow](../../.github/workflows/go.yml) runs lint, then `gotestsum -- ./...`. The `it` package starts WireMock through Testcontainers and needs a working Docker daemon; [its setup](../../it/shared/test_setup.go) owns container configuration. A container startup failure means integration coverage is unavailable, not that engine assertions passed.

For a focused engine change, run the relevant `pkg/engine`, node, result or execution-event tests first, then the complete suite. Contract work must also verify the Echopoint progress/SSE consumer and CLI flow-run result fixtures. Preserve the initial-input, skip-reason, assertion, module and output regressions already in the suite.

Documentation changes: check relative links, compare commands with the source/workflows above, and run `git diff --check`. There is no repository Markdown linter configured.
