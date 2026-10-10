# Domain context

| Term | Meaning in this repository |
| --- | --- |
| Flow | Directed graph of nodes, edges, inputs, assertions and final outputs executed by the engine. |
| Execution | One run of a flow with its own scheduling state and result. |
| Node | A typed operation; node `data` JSON uses the contract's snake_case field names. |
| Template reference | `{{ref}}` or `{{{ref}}}` in node data naming a flow input or `node_id.output`, optionally followed by a path such as `.0.id` into its value; see [template references](docs/technical/template-references.md). |
| Loop item | The element of a loop's `items` array injected into its body as the flow input `item` (or `item_var`) for one iteration. |
| Output view | Read-only snapshot of committed outputs taken when a node starts. |
| Control plane | Echopoint API owning jobs, authentication, tenant policy and accepted progress/SSE contracts. |
| Self-hosted runner | Long-lived process which claims jobs and reports progress/completion to the control plane. |
| Ephemeral runner | Short-lived execution initiated by a caller with flow-level authentication; it does not claim queued jobs. |
| Observer | Receives execution progress without becoming a control-flow decision hook. |
| User error | Flow-author error represented by `spi.UserError`; the engine reports it at debug rather than as a runner fault. |

The [architecture](docs/technical/architecture.md) identifies current implementation packages. Proposed `pkg/core` types do not replace the live vocabulary until consumers migrate.
