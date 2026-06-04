# Design: Dynamic template variables (`{{$...}}`)

Status: **proposal — not implemented.** Captures the design discussed while
building the anchor flow test suite. The motivating problems are name
collisions across re-runs (tests share one tenant; unique constraints) and the
verbosity of hardcoding test data.

## Goal

Postman-style built-in variables the runner generates at resolution time:

```
{{$randomUuid}}        {{$randomEmail}}      {{$runId}}
{{$timestamp}}         {{$randomFullName}}   {{$randomString:12}}
{{$isoTimestamp}}      {{$randomInt:1:100}}  {{$randomSlug}}
```

So a flow can write `eptest-org-{{$runId}}` (unique per execution, no cleanup
race) or `{{$randomEmail}}` for a product-user, instead of hardcoded
`eptest-org` that collides on the next run.

## Syntax

A leading `$` inside `{{ }}` marks a **dynamic generator**, reserving a
namespace that can't collide with node ids or input keys (neither may start with
`$`). Parameters follow the name, colon-separated:

```
{{$randomString}}      -> 16-char default
{{$randomString:12}}   -> 12 chars
{{$randomInt:1:100}}   -> integer in [1,100]
```

`{{{$randomUuid}}}` (triple-brace raw) still returns the raw value, same as
today's raw-substitution rule.

## Resolution order (in `pkg/node/template_resolver.go`)

For each `{{ X }}`:
1. `X` starts with `$` → **dynamic generator** (below).
2. `X` contains `.` → upstream node output (`{{nodeId.key}}`), unchanged.
3. otherwise → initial/env input, unchanged.

This slots in ahead of the existing input lookup; existing `{{x}}` /
`{{node.key}}` behaviour is untouched.

## The determinism problem (key design point)

The runner deliberately forbids `Date.now()` / `Math.random()` so an execution
can be **replayed/resumed** and produce the same result. Naive dynamic vars
reintroduce nondeterminism.

Resolution: derive everything from the **execution id**, which is already stable
per execution and unique across executions.

- Build a per-execution `dynamicContext` once, seeded by `hash(executionID)`:
  - a deterministic PRNG (e.g. PCG/xoshiro seeded from the hash),
  - the execution start time (captured once, persisted on the execution record
    so a replay re-injects the original instead of reading the clock),
  - `runId = short-hash(executionID)`.
- Two classes of generator:
  - **Stable per execution** — same value everywhere in one run:
    `{{$runId}}`, `{{$timestamp}}`, `{{$isoTimestamp}}`.
  - **Fresh per occurrence** — each use draws the next value from the seeded
    PRNG in document order: `{{$randomUuid}}`, `{{$randomString}}`,
    `{{$randomInt}}`, `{{$randomEmail}}`, names, slug.

Because the PRNG is seeded from the execution id and drawn in a fixed order, a
**replay of the same execution yields identical values**, while a **new
execution differs** — determinism preserved, collisions avoided.

> Note: today's resolver runs per-node and (under `parallel`) concurrently.
> A per-occurrence counter must therefore be assigned deterministically — e.g.
> pre-walk the definition in node/edge topological order and pre-assign each
> `{{$randomX}}` occurrence an index, rather than relying on wall-clock draw
> order. This pre-assignment is the main implementation cost.

## Build our own, or wrap a faker library?

Goal is a **ton of valid options** (IBAN, credit card, phone, address, …) with
two hard constraints: **deterministic** (seedable from the execution id) and
**no network**.

Honest assessment of the options:

- **Reimplement everything ourselves.** Full control, zero deps — but
  reimplementing ~200 generators plus locale data plus getting *validity* right
  (Luhn, IBAN mod-97, valid phone formats, real city/country tables) is a large,
  ongoing maintenance burden. Not worth it for the long tail (names, words,
  addresses, colours, user-agents).
- **Wrap `brianvoe/gofakeit` (v7, MIT).** ~200+ generators, no network, and —
  critically — a **seedable** faker: `gofakeit.New(seed)` gives an instance with
  its own `*rand.Rand`, so output is deterministic per seed. Luhn-valid credit
  cards, currencies, ACH, addresses, internet, company, etc. out of the box.
  Downside: a third-party dep, and a few validity-critical generators (notably
  **IBAN mod-97 / BIC**) may be missing or not checksum-valid — verify per type.

**Recommendation: a generator registry that is *our* surface, backed by gofakeit
for breadth and *our* code for the validity-critical financial ones.**

```go
// One small interface; the {{$...}} namespace is ours, the implementation is
// pluggable. This is the "our own generator" the team wants — without
// reimplementing 200 fakers.
type Generator func(ctx *dynamicContext, args []string) (string, error)

var registry = map[string]Generator{
    "iban":       genIBAN,        // OUR code: country + mod-97 checksum
    "bic":        genBIC,         // OUR code: valid BIC/SWIFT format
    "creditCard": genCreditCard,  // gofakeit (already Luhn-valid) + network arg
    "email":      genEmail,       // gofakeit
    // ... ~150 thin wrappers + a handful of our validity-critical ones
}
```

Every generator draws randomness only from `ctx` (the seeded PRNG / seeded
gofakeit instance), so the determinism story above holds regardless of whether a
given entry is hand-rolled or a gofakeit wrapper. If we ever want to drop the
dep, we swap implementations behind the registry without touching the `{{$...}}`
contract.

## Validity guarantees (the part that actually matters)

These must produce values that pass real validators, not just look right:

| Generator | Validity rule | Source |
|---|---|---|
| `{{$creditCard}}` / `:visa\|mastercard\|amex\|…` | passes **Luhn**, correct IIN prefix + length per network | gofakeit |
| `{{$creditCardCvv}}` | 3 digits (4 for amex) | gofakeit |
| `{{$iban}}` / `:FR\|DE\|…` | ISO 13616: country + **mod-97 == 1**, correct length per country | **ours** |
| `{{$bic}}` | 8 or 11 chars, valid bank/country/location structure | **ours** |
| `{{$phone}}` / `:E164\|US\|…` | **E.164** by default (`+<cc><national>`) | ours over gofakeit |
| `{{$uuid}}` | RFC 4122 v4 | seeded PRNG |
| `{{$email}}` | RFC-5322-safe local + reserved test domain (`@example.test`) | gofakeit |

## Generator catalogue (target)

Stable per execution: `{{$runId}}`, `{{$timestamp}}`, `{{$isoTimestamp}}`,
`{{$today}}`. Everything else is fresh per occurrence (seeded PRNG, fixed draw
order — see determinism section).

**Identity & contact**
`firstName lastName fullName username gender jobTitle ssn`
`email phone:E164 phoneFormatted`

**Address**
`street city state stateAbbr country countryAbbr zip latitude longitude
fullAddress timezone`

**Finance** (validity-critical, see table)
`creditCard:<network> creditCardCvv creditCardExp iban:<cc> bic
currency:code currency:name price:<min>:<max> achRouting achAccount
bitcoinAddress amount:<min>:<max>`

**Internet & tech**
`url domain ipv4 ipv6 mac userAgent httpMethod httpStatusCode emoji
md5 sha256 base64:<n> jwt`

**Commerce & company**
`company companySuffix productName productCategory sku ean13 barcode`

**Text & primitives**
`word words:<n> sentence paragraph lorem:<n> color hexColor
int:<min>:<max> float:<min>:<max> bool digit:<n> string:<n> slug
uuid nanoid`

**Date & time**
`runId timestamp isoTimestamp today futureDate pastDate
date:<layout> weekday month`

(~80 listed; the registry can carry the full gofakeit surface — this is the
curated, documented subset.)

## Parameters

Colon-separated after the name; each generator declares its own params:

```
{{$string:12}}              {{$int:1:100}}          {{$price:9.99:199.99}}
{{$creditCard:visa}}        {{$iban:FR}}            {{$words:5}}
{{$phone:E164}}             {{$date:2006-01-02}}    {{$currency:code}}
```

Unknown generator name or bad params → a clear resolution error naming the
node and the offending `{{$...}}` (not a silent empty string).

## Open questions

- Persist the execution start time for true replay vs "fresh each run"
  (sufficient for the test-suite use case).
- Locale: default `en`, optional `{{$city:fr}}`? gofakeit is mostly en — defer.
- CLI discoverability: `echopoint flows vars [--category finance]` to list the
  registry; `flows validate` must treat `{{$...}}` as always-resolvable.

## Why this shape

- `$` prefix = zero collision risk, instantly recognisable (Postman/Insomnia).
- Seeding from the execution id keeps the runner's replay guarantee — the whole
  reason `Date.now()`/random are banned today.
- A **registry behind our own `{{$...}}` namespace** gives us the team's "own
  generator" control and a swap-out path, while gofakeit supplies the breadth so
  we only hand-write the few generators where *validity* is the point.
