# Request bodies

A request node sends its `body` after it resolves `{{…}}` templates. `body_encoding` chooses how:

- Empty (the default): a string body is sent as it is; any other body is sent as JSON. Without a `Content-Type` header, the runner sets `application/json` when the payload is valid JSON.
- `form`: an object body is sent as `application/x-www-form-urlencoded`. Nested objects and lists use brackets, as Stripe and Rails read them: `{"metadata":{"plan":"pro"},"items":[{"price":"price_1"}]}` becomes `metadata[plan]=pro&items[0][price]=price_1`. A string body is taken as already encoded. Without a `Content-Type` header, the runner sets `application/x-www-form-urlencoded`.

The runner encodes a form body after templates resolve. A body encoded beforehand would carry `{{signup.email}}` inside an encoded string, and a resolved `garbage+tag@nanostack.dev` would then read as `garbage tag@…` on the server, because `+` means a space in a form.

A runner older than this support sends a `form` body as JSON. Every claim therefore declares the `form_body` capability ([`controlplane.Capabilities`](../../internal/controlplane/client.go)), and the control plane refuses a job that uses a form body to a claim without it, as it does for `secret_outputs` ([secret redaction](secret-redaction.md)).
