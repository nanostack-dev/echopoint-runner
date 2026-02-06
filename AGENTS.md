# Agent Guide: Echopoint Flow Engine

Engine for processing webhook events and logical flows.

## Key Responsibilities
- Event evaluation and transformation.
- Flow logic execution via JSONPath.

## Tech Stack
- **Language**: Go
- **Libraries**: zerolog, jsonpath

## Best Practices
- Optimize JSONPath queries for performance.
- Ensure thorough unit testing for complex flow logic.
- Maintain idempotency in event processing.

## Tools & MCP
- When working with external libraries, **use the Context7 MCP** for accurate usage and API details.

## Git Conventions
- **Commit Messages**: Follow [Conventional Commits](https://www.conventionalcommits.org/):
  - `feat: add new flow node type`
  - `fix: resolve JSONPath evaluation bug`
  - `perf: optimize flow execution path`
- **Branch Naming**: When working on tracked tasks, include ticket number:
  - Format: `<type>/<TICKET-ID>-<description>`
  - Examples: `feat/ENG-10-add-delay-node`, `fix/ENG-20-jsonpath-bug`
  - For untracked work: `<type>/<description>` (e.g., `refactor/simplify-engine`)
