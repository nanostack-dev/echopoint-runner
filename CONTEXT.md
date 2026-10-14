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
| Secret input | A flow input named in `secret_input_keys`; its value is replaced by `***` in every result, progress event and error the run reports. See [secret redaction](docs/technical/secret-redaction.md). |
| Secret output | A node output declared with `"secret": true`; from the moment the node extracts it, its value is masked like a secret input wherever it appears, including module outputs and downstream inputs that carry it. |
| HTTP call | One outbound HTTP request a node sent during an execution; each loop iteration, poll attempt and retry is its own call. Control-plane reads such as webhook-wait polling are not HTTP calls. A top-level node's result lists its calls, its bodies' included, in `http_calls`. |
| HTTP call limit | The most HTTP calls one execution may send (`max_http_calls_per_execution`). The node whose call would pass it fails with `HTTP_CALL_LIMIT_EXCEEDED`. |
| Response budget | The response body bytes, with their parsed form, that every request node sharing a runner may hold at once (`ECHOPOINT_MAX_BUFFERED_RESPONSE_BYTES`). A node that cannot reserve its body waits within its request timeout. |

The [architecture](docs/technical/architecture.md) identifies current implementation packages. Proposed `pkg/core` types do not replace the live vocabulary until consumers migrate.
