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
