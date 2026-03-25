# Parallel Execution Model

This document explains how parallel flow execution works in `echopoint-runner` after the scheduler and `OutputView` changes.

## Core Rule

- Nodes in the same ready batch may run in parallel.
- They can read only outputs that were committed by earlier batches.
- They cannot observe sibling in-flight updates.
- Children run only after the whole parent batch finishes and commits.

## Example Flow

```mermaid
graph LR
    I[initialInputs]
    A[branch-1]
    B[branch-2]
    C[branch-3]
    D[branch-4]
    E[branch-5]
    F[branch-6]
    J[join]

    I --> A
    I --> B
    I --> C
    I --> D
    I --> E
    I --> F

    A --> J
    B --> J
    C --> J
    D --> J
    E --> J
    F --> J
```

## Scheduler Phases

```mermaid
flowchart TD
    S[Start iteration] --> R[Collect ready nodes]
    R --> V[Build OutputView snapshots from committed outputs]
    V --> P[Run ready batch in parallel]
    P --> W[Wait for full batch completion]
    W --> C[Commit copied node outputs to engine state]
    C --> D[Decrease successor dependency counts]
    D --> N[Next ready batch becomes eligible]
```

## Safe Visibility Model

```mermaid
sequenceDiagram
    participant Engine
    participant Batch as Ready Batch A,B,C
    participant State as Committed Outputs
    participant Join as Child Node

    Engine->>State: read committed outputs only
    Engine->>Batch: give each node its own read-only OutputView snapshot
    par branch-1
        Batch->>Batch: A executes
    and branch-2
        Batch->>Batch: B executes
    and branch-3
        Batch->>Batch: C executes
    end
    Batch-->>Engine: return results
    Engine->>State: commit copied outputs for A, B, C
    Engine->>Engine: decrement dependency counters
    Engine->>Join: schedule child only after batch commit
```

## Why `OutputView` Exists

Before the fix, `ExecutionContext.AllOutputs` was a mutable nested map. That created a correctness risk in parallel execution because a node could accidentally mutate shared engine state.

Now the engine passes a read-only snapshot view instead:

```mermaid
flowchart LR
    S[Engine committed state\nmap[nodeID][outputKey]value] --> O[OutputView snapshot]
    O --> A[Node A]
    O --> B[Node B]
    O --> C[Node C]

    A -. cannot mutate .-> S
    B -. cannot mutate .-> S
    C -. cannot mutate .-> S
```

## Behavioral Guarantees

- Independent siblings do not need to see each other's updates.
- If a node must see another node's output, that relationship must be expressed as a dependency edge.
- Output publication happens after the full batch barrier, not during sibling execution.
- Committed outputs are copied before being stored for future batches.

## Practical Consequence

The engine now behaves like a deterministic DAG executor:

1. run independent work together,
2. commit results together,
3. unlock dependent work after commit,
4. never expose live mutable shared output state to running nodes.
