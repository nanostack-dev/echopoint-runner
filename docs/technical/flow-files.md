# Flow files

[`pkg/flowdsl`](../../pkg/flowdsl) compiles a YAML flow file into the create-flow JSON the Echopoint API accepts, and decompiles a stored flow back into a file. It is a separate Go module: Echopoint pins it in two `go.mod` files and the echopoint CLI embeds it, so a change here reaches them only after they bump the pin. [`pkg/flowdsl/wire`](../../pkg/flowdsl/wire/types.go) is a hand-maintained subset of Echopoint's OpenAPI models; a node field it lacks is dropped on decompile.

The full key reference for authors lives with the consumers: Echopoint's MCP flow-file reference (`internal/mcp/reference.go`) and the CLI's `docs/flows.md`.

## Request fields

A `request` node takes `method`, `url`, `headers`, `query`, `body`, `body_encoding` and `timeout_ms`. `body_encoding` accepts only `form` and lowers to the node's `data.body_encoding` ([request bodies](request-bodies.md)); any other value is a compile error. Decompile writes it back when the stored node has it.

A CLI that embeds a flowdsl older than this field refuses a file that carries it with `node "<id>": unknown field "body_encoding"`. Before this field, decompile dropped it, so a pulled and pushed form-body flow sent JSON.
