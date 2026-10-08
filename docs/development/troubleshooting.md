# Troubleshooting

| Symptom | Verified cause and repair | Verification |
| --- | --- | --- |
| Integration setup fails before tests | `it/shared/test_setup.go` starts WireMock with Testcontainers. Start an accessible Docker daemon and retry. | `go test ./it` reaches and runs the integration assertions. |
| Node configuration appears absent | Node JSON uses snake_case; unknown fields are not rejected. Compare the data to the consumer's `*NodeData` contract and correct the key. | A focused decode/execution regression observes the intended field. |
| A consumer still runs old engine behavior | Echopoint and CLI pin the library in their own `go.mod`; releasing this repository does not update them. Follow the consumer upgrade in the publication runbook. | The consumer's module version and its execution tests match the release. |
| Local startup reports a required variable | `config.Load` requires organization ID, runner API key and runner ID. Supply the missing value through environment or untracked `.env`. | `serve` starts against the intended organization and target without a configuration error. |

Record new recurring issues only after establishing the cause and successful verification. Keep tokens and event payloads out of examples.
