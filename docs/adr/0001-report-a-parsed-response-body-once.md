# 0001. Report a parsed response body once

- Status: accepted
- Date: 2026-11-19

## Context

A request result (`node.RequestExecutionResult`) reported every response body of up to 1 MiB twice: `response_body`, the raw bytes encoded as base64 because the Go field is `[]byte`, and `response_body_parsed`, the JSON tree or the body as text. Base64 adds a third to the size, so each stored and served result was about 2.3 times its body. A flow that reads another execution's node result through the Echopoint API compounds this: its own result holds the base64 of a result that already holds a base64 body. The 2026-11-19 Echopoint audit found this in node results returned by the API and the MCP tools (finding `node-result-payload-bloat`).

The consumers of a stored node result, checked on 2026-11-19:

- Echopoint stores the result as an opaque object. The OpenAPI `NodeExecutionResult.result` is `additionalProperties: true`. The `response_body` field in the OpenAPI is the collection `ExecutedRequestResponse`, which Echopoint builds itself, not a node result.
- The frontend (`node-result-detail.tsx`) shows `response_body_parsed` when it is present and decodes `response_body` only as a fallback.
- The CLI and MCP pass the result through without reading `response_body`.
- Templates and extractors address node outputs, not result fields.

## Decision

The runner leaves `response_body` out of a result whose parsed form carries the body: the parsed form is present, and the body is valid UTF-8 with no NUL byte (`node.parsedCarriesBody`). It keeps the raw bytes when the parsed form would alter or lose them: a body that is not UTF-8, a body with a NUL byte (Echopoint stores NUL as U+FFFD), a JSON body with a string escape of NUL (`\u0000`) or of an unpaired surrogate (such as `\ud800`, which `encoding/json` decodes to U+FFFD), a JSON `null` (which parses to nothing) and a body truncated at 1 MiB (which has no parsed form). A valid escaped surrogate pair, such as an emoji, is decoded without loss and does not keep the raw bytes.

A failed request result whose parsed form is a JSON tree also keeps the raw bytes (`RequestExecutionResult.Fail`). This covers a failed assertion, output extraction or output validation. Regex outputs and assertions read the wire bytes, and a string operator on a body that parses to a JSON object or array matches the wire text (`BodyExtractor.ExtractText`). The tree cannot explain why such a check failed: it has no spacing, no number digits past float64 (`10.50` becomes `10.5`, a 19-digit id is rounded) and no escapes. A text body is its own parsed form, so a failed text result still reports it once.

## Alternatives

- **Echopoint drops the duplicate when it stores or serves a result.** No runner release is needed, but the runner still sends both forms in progress events and completions, and every self-hosted runner and CLI run keeps paying for them. Echopoint would also need the same lossless rule. Rejected because the runner owns the result shape.
- **Keep the raw body and drop the parsed form.** The frontend and Echopoint's integration tests read `response_body_parsed`, and base64 stays a third larger than text. Rejected.
- **Report the raw body as text instead of base64.** This changes the type of an existing field that the frontend and redaction decode as base64, and still sends the body twice. Rejected.
- **A runner setting or a claim capability that turns the change on.** Every current consumer already reads the parsed form first, so there is nothing to negotiate. A flag would stay on forever. Rejected; a revert is the rollback.

## Consequences

- A request result for a JSON or text body is up to about 55 percent smaller in progress events, completions, storage and API or MCP reads. Nested reads stop compounding.
- The report of a successful JSON result is its parsed tree. JSONPath assertions and outputs evaluate against that tree, but regex checks and string operators on a JSON object or array read the wire bytes. The tree does not keep whitespace, key order, a leading byte order mark, duplicate keys, escapes or the digits of a number past float64 precision, so a passing result does not report the exact bytes of a JSON response. A failed one does.
- `response_body` is now absent for most results. A consumer must read `response_body_parsed` first and fall back to decoding `response_body`. Results stored before the change keep both fields until retention removes them; no backfill is needed.
- Secret redaction still decodes and masks a base64 `response_body`. That path now runs for bodies the parsed form cannot carry and for failed JSON results.
- The Echopoint frontend shows the HTML preview of a response only when `response_body_parsed` is absent, so it shows it only for a truncated HTML body. This was true before the change and is not changed by it.
