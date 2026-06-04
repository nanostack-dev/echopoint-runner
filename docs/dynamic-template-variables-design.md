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

## Generator catalogue (initial)

| Variable | Class | Result |
|---|---|---|
| `{{$runId}}` | stable | short id derived from execution id |
| `{{$timestamp}}` | stable | unix seconds at execution start |
| `{{$isoTimestamp}}` | stable | RFC3339 at execution start |
| `{{$randomUuid}}` | per-occurrence | UUIDv4 (from seeded PRNG) |
| `{{$randomInt}}` / `:min:max` | per-occurrence | integer, default [0,1000] |
| `{{$randomString}}` / `:N` | per-occurrence | N-char alnum, default 16 |
| `{{$randomEmail}}` | per-occurrence | `user-<rand>@example.test` |
| `{{$randomFirstName}}` / `LastName` / `FullName` | per-occurrence | from a small built-in word list |
| `{{$randomSlug}}` | per-occurrence | `kebab-three-words` |

Faker data is a small static word list compiled into the runner — no external
dependency, no network.

## Open questions

- Persisting the execution start time for true replay vs accepting "fresh each
  run" (sufficient for the test-suite use case).
- Whether to expose a `{{$env:NAME}}` escape hatch (probably no — env overlays
  already cover that).
- CLI discoverability: an `echopoint flows vars` listing, and `flows validate`
  should treat `{{$...}}` as always-resolvable (never an "unknown reference").

## Why this shape

- `$` prefix = zero collision risk, instantly recognisable to anyone who's used
  Postman/Insomnia.
- Seeding from the execution id keeps the runner's replay guarantee, which is
  the whole reason `Date.now()`/random are banned today.
- Static faker word list keeps the runner dependency-free and deterministic.
