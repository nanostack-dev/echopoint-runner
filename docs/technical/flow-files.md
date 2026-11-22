# Flow files

[`pkg/flowdsl`](../../pkg/flowdsl) compiles a YAML flow file into the create-flow JSON the Echopoint API accepts, and decompiles a stored flow back into a file. It is a separate Go module: Echopoint pins it in two `go.mod` files and the echopoint CLI embeds it, so a change here reaches them only after a `pkg/flowdsl/vX.Y.Z` tag and their pin bump ([deployment](../runbooks/deployment.md)). [`pkg/flowdsl/wire`](../../pkg/flowdsl/wire/types.go) is a hand-maintained subset of Echopoint's OpenAPI models; a node field it lacks is dropped on decompile.

The full key reference for authors lives with the consumers: Echopoint's MCP flow-file reference (`internal/mcp/reference.go`) and the CLI's `docs/flows.md`.

## `on_failure`

`on_failure: <node>` compiles to an edge of type `failure`. The target runs only when the source node fails; when the source succeeds or is skipped, the target is skipped as `routed_away_by_branch`, and a chain reached only through it is skipped too (with `not_reachable_after_main_phase`, as below a routed-away branch arm). The target needs `run_when: always`, because an on_success node is skipped after any upstream failure; compile warns otherwise. The [parallel execution model](../parallel-execution-model.md#failure-edges-on_failure) owns the scheduling rule.

## Request fields

A `request` node takes `method`, `url`, `headers`, `query`, `body`, `body_encoding` and `timeout_ms`. `body_encoding` accepts only `form` and lowers to the node's `data.body_encoding` ([request bodies](request-bodies.md)); any other value is a compile error. Decompile writes it back when the stored node has it.

A CLI that embeds a flowdsl older than this field refuses a file that carries it with `node "<id>": unknown field "body_encoding"`. Before this field, decompile dropped it, so a pulled and pushed form-body flow sent JSON.

## Flow header: absent and empty differ

`description` and `tags` lower only when the file writes them. An absent key leaves `description` and `tags` out of the create request. A key written empty (`description: ""`, a YAML null such as `description:` or `description: ~`, or `tags: []`) lowers to `"description": ""` (on the request and its `flow_definition`) and `"tags": []`, so an update can tell "clear it" from "keep it". Only `tags: []` clears tags: a YAML null `tags:` or `tags: ~` is the compile error `tags: line N: expected a list`, because a stray empty key that cleared tags could drop a flow from a tag-selected monitor. `role` needs no empty form: write `role: test` to stop a flow being a function.

Echopoint decides what an update from a file does with an absent key; flowdsl only keeps the difference.

## Compile refusals

Compile reports these as Error diagnostics, so the file produces no flow:

| Check | Message |
| --- | --- |
| An edge (`next`, a branch `target` or `default`, or `on_failure`) leads back to a node on its own path, including a node to itself | `node "b" next "a" makes a cycle: a -> b -> a` |
| The file holds a second YAML document after `---` (a trailing empty `---` or `...` is allowed) | `the file holds more than one YAML document; write one flow per file` |
| A tag that, trimmed and lowercased, does not match `^[a-z0-9][a-z0-9._:/-]{0,19}$` | `tag "has space" is invalid: tags must be lowercase, 1-20 characters, and match [a-z0-9][a-z0-9._:/-]*` |
| More than 32 distinct tags after lowercasing | `a flow may have at most 32 tags, got 33` |

The engine counts every edge type before it starts a node, so a cyclic graph never runs: the run fails with `cycle detected or unreachable nodes` ([`pkg/engine`](../../pkg/engine/execution.go)). A poll or loop body is its own graph and is checked on its own; repeat work with those nodes, never with a back edge. Before this check a cyclic file compiled, saved and failed only at run time.

Before the document check, compile read only the first document and dropped everything after `---` without a diagnostic.

The tag checks copy Echopoint's canonical rule in `internal/feature/flows/tags.go` (`normalizeTags`); change both together. Compile cannot check the per-organization limit of 100 distinct tags, which only a save knows. Before the tag check, an invalid tag compiled and failed only at save with 400 `INVALID_TAG`; once Echopoint pins this flowdsl, a save from source refuses it earlier with 400 `INVALID_FLOW_DEFINITION` carrying the message above.

A flow saved as JSON (the UI, or `create_flow` with a `flow_definition`) does not go through flowdsl, so Echopoint's save path still accepts a cyclic graph until it gets its own check.
