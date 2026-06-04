# Echopoint Runner Agent Guide

Scope: Go execution engine for webhook events and logical flows consumed by `../echopoint`.

## Focus Areas

- Event evaluation, transformations, flow execution, JSONPath handling.
- Exported progress/execution events may require matching `echopoint/cmd/http/openapi.yaml` updates.

## Invariants

- Keep event processing idempotent.
- Optimize JSONPath paths only with tests or measured need.
- Avoid app/control-plane policy here; `echopoint` owns accepted API/SSE contracts.

## Verification

- Run narrow Go tests for changed packages.
- For event shape changes, check the consuming `echopoint` contract in the same work session.

## Git

- Conventional Commits.
- Branches: tracked `<type>/<TICKET-ID>-<description>`, untracked `<type>/<description>`.
