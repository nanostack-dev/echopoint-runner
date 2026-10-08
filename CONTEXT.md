# Domain context

| Term | Meaning in this repository |
| --- | --- |
| Flow | Directed graph of nodes, edges, inputs, assertions and final outputs executed by the engine. |
| Execution | One run of a flow with its own scheduling state and result. |
| Node | A typed operation; node `data` JSON uses the contract's snake_case field names. |
| Output view | Read-only snapshot of committed outputs taken when a node starts. |
| Control plane | Echopoint API owning jobs, authentication, tenant policy and accepted progress/SSE contracts. |
| Self-hosted runner | Long-lived process which claims jobs and reports progress/completion to the control plane. |
| Ephemeral runner | Short-lived execution initiated by a caller with flow-level authentication; it does not claim queued jobs. |
| Observer | Receives execution progress without becoming a control-flow decision hook. |
| User error | Flow-author error represented by `spi.UserError`; report at warn rather than as a runner fault. |

The [architecture](docs/technical/architecture.md) identifies current implementation packages. Proposed `pkg/core` types do not replace the live vocabulary until consumers migrate.
