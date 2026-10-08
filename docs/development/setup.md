# Local setup

Use the Go version in [go.mod](../../go.mod), currently 1.26.5. Docker is required for the WireMock integration suite; tests and builds do not need sibling repositories. Install `golangci-lint` for linting; CI's tool setup is in [go.yml](../../.github/workflows/go.yml).

From this repository root:

```sh
go mod download
go build -o /tmp/echopoint-runner ./cmd/runner
/tmp/echopoint-runner --help
```

For a long-lived local runner, copy [.env.example](../../.env.example) to an untracked `.env`, then supply a real organization ID, runner API key and runner ID through your approved secret mechanism. Confirm the API target before starting:

```sh
go run ./cmd/runner serve
```

The default target is the development API; [.env.example](../../.env.example) and [config.go](../../internal/config/config.go) define the environment variables. Process environment values take precedence over `.env`. This startup claims real jobs; use a development organization. Ephemeral execution uses different inputs and authentication, documented in [ephemeral mode](../ephemeral-mode.md).
