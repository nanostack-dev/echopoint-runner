# Email wait

An `email_wait` node reads the run inbox (ADR-0030 in Echopoint). Launch creates the inbox and injects `inbox.address`, `inbox.local`, `inbox.domain` and `inbox.messages_url`. The node polls `inbox.messages_url` with the job token (and the Job's trace context, see [control-plane calls](architecture.md#control-plane-calls)) every 250 ms until `timeout_ms` (default 60000). Each poll after the first sends `after`, the id of the last message the node read, so the API returns only the messages stored since; the node keeps the earlier ones. An API that ignores `after` returns every message again, and the node skips the ones it knows. A timeout above 7200000, the lifetime of the inbox address, is rejected when the flow is parsed. The runner declares the `email_wait` capability on every claim.

## What an assertion reads

The node's assertions are checked on each message, oldest first:

- `json_path` reads the message as its outputs (below): `$.subject`, `$.envelope_to`, `$.code`, `$.first_link`.
- `body` and `regex` read the text: the text part, or the HTML part rendered as text when the message has no text part.
- `html` reads the HTML part with a CSS selector.
- `header` reads any header of the message by name: `From`, `To`, `Cc`, `Reply-To`, `Subject`, `Message-Id`, `X-...`. A header the API does not send reads as empty.

Without an assertion, the first message matches: the run inbox only receives the mail sent to this run. Match a `+tag` with `$.envelope_to contains +alice@`.

## First match

Without `expect`, the wait completes on the first message that passes the assertions. Its outputs are `id`, `message_id`, `from`, `to`, `cc`, `envelope_from`, `envelope_to`, `subject`, `bcc`, `text`, `html`, `links`, `first_link`, `code` and `received_at`. Every key is always present: `cc` is an empty string when the message has no Cc, `bcc` is `true` when `envelope_to` is in neither `To` nor `Cc` (compared in full, ignoring case; `false` without an envelope recipient), `code` and `first_link` are empty strings and `links` is an empty list when the message has none.

- `links` are the `http` and `https` hrefs of the HTML `<a>` elements in document order, with entities decoded and duplicates removed, or the URLs of the text when there is no HTML. `link_pattern` keeps the links it matches.
- `code` is the first match of `code_pattern` (default `\b\d{6}\b`) in the text: capture group 1 when the pattern has a group, else the whole match.

Declared outputs extract from the matched message with any extractor, and a declared output marked `secret` is masked like any secret output: `outputs: [{name: code, secret: true, extractor: {type: json_path, path: $.code}}]`.

A timeout fails the node with `EMAIL_WAIT_TIMEOUT`. The result keeps `closest`, the message that passed the most assertions with each check and the first 4 KiB of its `text` (the run sweep deletes the inbox). `recent_messages` lists the five newest messages the wait saw, on a match those before the matched one, without their bodies but each with the `checks` it passed and failed. A check's `actual` text is cut at 4 KiB. A rejected token or an ended run (401, 403, 404) fails it at once with `EMAIL_WAIT_FAILED`.

## Expected events

With `expect`, the wait judges named groups exactly as a [webhook wait](webhook-wait.md#expected-events) does, with `min`, `max` and `settle_ms`. The only output is `received`. A failed group fails the node with `EMAIL_WAIT_EXPECTATIONS_FAILED`; `request_ids` and `closest.request_id` hold message ids.

## HTML and regex extractors

Both extractors work on any result with a body, a request response included:

- `{"type": "html", "selector": "a.verify", "attribute": "href", "all": false}` parses the HTML as a browser does, so malformed markup never fails. It returns the text of the first match, or its `attribute`; with `all`, a list over every match.
- `{"type": "regex", "pattern": "code: (\\w+)", "group": 1}` returns the first match in the raw body. `group` defaults to 1 when the pattern has a group, else 0.
