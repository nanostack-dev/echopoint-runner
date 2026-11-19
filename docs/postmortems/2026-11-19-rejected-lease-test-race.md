# Runner v0.112.0 released but not deployed: a rejected-lease race failed main CI

| | |
|---|---|
| **Date** | 2026-11-19 |
| **Severity** | SEV4 |
| **Status** | In review |
| **Service** | echopoint-runner |
| **Authors** | Nanostack routine (echopoint-investigation) |
| **Duration** | ~2h 21m (trigger to resolution) |
| **Time to detect** | ~1h 06m |
| **Time to mitigate** | ~1h 31m |

> Blameless: this document names systems, processes and missing guards, not people.

## Summary

The merge of echopoint-runner#126 (transport retries) released tag `v0.112.0` at 17:07Z, but the main CI test job failed on `TestRejectedLeaseStopsTheRunBeforeItsNextRequest`, so no container image was built and no deploy was dispatched. The dev and prod self-hosted runner services kept running v0.111.0 (`main-d159a21`), while echopoint-cli v5.0.1 already embeds v0.112.0. The test failed because `markJobRejected` cancelled the run before it recorded the rejection, so the cancelled run checked `isRejected`, saw false and sent a failure completion. The race existed since the first runner snapshot and failed CI once in about 40 main runs. The fix records the rejection first; it ships with this postmortem and deploys the retries.

## Impact

No customer impact. Self-hosted runs on dev and prod (monitors, the public status probe, the self-hosted CI monitor) kept running without the transport retries of v0.112.0 for about 1.5 h, while Ephemeral runs through CLI v5.0.1 had them. In production the race had no effect: every one of the 35 heartbeat rejections since 2026-10-20 came after the job had finished.

- **1 of ~40** main CI runs failed on this race
- **0** runner services on v0.112.0 after the release
- **35** prod+dev lease rejections in 30 days, all after completion

## Trigger

Main CI run [44573501171](https://github.com/nanostack-dev/echopoint-runner/actions/runs/44573501171) for caf812c failed `TestRejectedLeaseStopsTheRunBeforeItsNextRequest`; the image build and the dev deploy dispatch were skipped.

## Detection

The `echopoint-investigation` routine saw at 18:1xZ that the runner services still logged `app.commit` `main-d159a21` an hour after the `v0.112.0` release. A failed main CI on the runner notifies nobody, and the PR body said the merge deploys the runners.

## Resolution

The fix records the rejection in the rejected map before it cancels the run, so a cancelled run always sees the rejection and sends no completion. Its merge releases the next tag, builds the image and deploys both fixes.

## Timeline (UTC)

| Time | | Event |
|---|---|---|
| 17:33:47 | T−191d 23h | **markJobRejected cancels before it records the rejection** Present since the first snapshot, c109bdf. ([link](https://github.com/nanostack-dev/echopoint-runner/commit/c109bdf)) |
| 21:29:58 | T−35d 19h | **Test for rejected leases added (6cffb71)** ([link](https://github.com/nanostack-dev/echopoint-runner/commit/6cffb71)) |
| 17:06:12 | T−2m 03s | **echopoint-runner#126 merged (caf812c)** PR checks were green. ([link](https://github.com/nanostack-dev/echopoint-runner/pull/126)) |
| 17:07:08 | T−1m 07s | **Release workflow tags v0.112.0** The Release workflow runs on every push to main and does not wait for tests. ([link](https://github.com/nanostack-dev/echopoint-runner/releases/tag/v0.112.0)) |
| 17:08:15 | T+0s | **Main test job fails; image build and deploy skipped** `An error is expected but got nil` at runtime_test.go:270; 763 tests, 1 failure. ([link](https://github.com/nanostack-dev/echopoint-runner/actions/runs/44573501171)) |
| 17:22:34 | T+14m 19s | **echopoint-cli v5.0.1 released with runner v0.112.0** ([link](https://github.com/nanostack-dev/echopoint-cli/pull/109)) |
| 18:00:24 | T+52m 09s | **Runner services still log main-d159a21** VictoriaLogs `app.commit` on `echopoint-runner-prod_runner` and `-dev_runner`. |
| ~18:15 | T+1h 06m | **Routine finds the release was never deployed** |
| ~18:40 | T+1h 31m | **Race reproduced; fix passes all tests and lint** |
| ~19:30 | T+2h 21m | **Fix merged and runner services deployed** |

## Root cause analysis: the five whys

Start at the impact and ask why until the answer is something the team can change. Solid edges ask why it happened; dashed edges ask why the impact was as large as it was.

```mermaid
flowchart TD
  P["Runner v0.112.0 was released and pinned by CLI v5.0.1, but the dev and prod runner services never got it"]
  W1["The main CI test job failed, so `Build and Push Container Image` and `Dispatch Deploy (dev)` were skipped"]
  W2["`TestRejectedLeaseStopsTheRunBeforeItsNextRequest` got a nil error: the rejected run sent a failure completion"]
  W3["`markJobRejected` cancels the run before it writes the rejected map, and the woken run checks `isRejected` in that gap"]
  W4["The test leaves the gap to the scheduler: it passed 400 local runs and the PR check, and failed once in about 40 main runs"]
  R1["R1 · A rejection is two pieces of state written in the wrong order: the signal (cancel) before the fact (rejected map) it reports"]
  W5["The release tag exists although the code failed its tests, so a consumer pinned an undeployed version"]
  W6["Release and Go Build and Test are separate workflows on the same push, and Release does not wait for the tests"]
  R2["R2 · Release tagging is not gated on a green main test run"]
  W7["Nobody saw the skipped deploy for an hour, and the CLI release went ahead"]
  W8["A failed main CI on echopoint-runner sends no alert, and no check compares the runner services' version with the latest release"]
  R3["R3 · Nothing checks that a released runner version reaches the runner services"]
  P -->|why?| W1
  W1 -->|why?| W2
  W2 -->|why?| W3
  W3 -->|why?| W4
  W4 -->|why?| R1
  P -->|why?| W5
  W5 -->|why?| W6
  W6 -->|why?| R2
  P -.->|why so bad?| W7
  W7 -->|why?| W8
  W8 -->|why?| R3
  class P problem
  class R1,R2,R3 root
  classDef problem fill:#fdecea,stroke:#c2362f,stroke-width:2px,color:#4a1210
  classDef root fill:#e7f5ec,stroke:#1f7a45,stroke-width:2px,color:#0f3a20,font-weight:bold
  classDef open fill:#fff4d9,stroke:#9a6200,stroke-dasharray:5 4,color:#3f2a00
```

- **Problem:** Runner v0.112.0 was released and pinned by CLI v5.0.1, but the dev and prod runner services never got it
  - **Why?** The main CI test job failed, so `Build and Push Container Image` and `Dispatch Deploy (dev)` were skipped _Evidence: [Run 44573501171](https://github.com/nanostack-dev/echopoint-runner/actions/runs/44573501171): Test failure, the two later jobs skipped_
    - **Why?** `TestRejectedLeaseStopsTheRunBeforeItsNextRequest` got a nil error: the rejected run sent a failure completion _Evidence: Log: `runner job failed` with `context canceled`, no `skipping failure completion for rejected runner job` line_
      - **Why?** `markJobRejected` cancels the run before it writes the rejected map, and the woken run checks `isRejected` in that gap _Evidence: `internal/runtime/runtime.go` markJobRejected; a 50 ms sleep after the cancel fails the test 5 of 5 times, and passes with the fix_
        - **Why?** The test leaves the gap to the scheduler: it passed 400 local runs and the PR check, and failed once in about 40 main runs _Evidence: Main `Go Build and Test` history since 2026-10-07: first failure on caf812c_
          - **Why?** A rejection is two pieces of state written in the wrong order: the signal (cancel) before the fact (rejected map) it reports → **Root cause R1** (fixed by A1)
  - **Why?** The release tag exists although the code failed its tests, so a consumer pinned an undeployed version _Evidence: `release.yml` runs on every push to main; v0.112.0 tagged at 17:07:08Z, tests failed at 17:08:15Z_
    - **Why?** Release and Go Build and Test are separate workflows on the same push, and Release does not wait for the tests _Evidence: [release.yml](https://github.com/nanostack-dev/echopoint-runner/blob/main/.github/workflows/release.yml) trigger `push: branches: [main]`_
      - **Why?** Release tagging is not gated on a green main test run → **Root cause R2** (fixed by A2)
  - **Why so bad?** Nobody saw the skipped deploy for an hour, and the CLI release went ahead _Evidence: CLI v5.0.1 at 17:22Z; PR body of #126 says the merge deploys the runners_
    - **Why?** A failed main CI on echopoint-runner sends no alert, and no check compares the runner services' version with the latest release
      - **Why?** Nothing checks that a released runner version reaches the runner services → **Root cause R3** (fixed by A3, A4, A5)

## Root causes

- **R1** A rejection is two pieces of state written in the wrong order: the signal (cancel) before the fact (rejected map) it reports
- **R2** Release tagging is not gated on a green main test run
- **R3** Nothing checks that a released runner version reaches the runner services

## Contributing factors

- Every push to main releases a version, so the tag is cut minutes before the tests finish.

## Lessons learned

### What went well

- A 50 ms sleep at the suspected gap reproduced the failure at once, which proved the cause before the fix.
- Lease rejections in production all came after completion, so the race never sent a wrong completion there.

### What went wrong

- The merge author released the CLI on the assumption that the merge had deployed the runners.
- A red main run on a merged commit was the only signal.

### Where we got lucky

- The skipped deploy only delayed a retry improvement; a skipped security fix would have stayed undeployed the same way.
- No production lease was rejected mid-run in 30 days, where the race would have sent a completion for a reassigned job.

## Action items

| ID | Action | Type | Owner | Due | Status | Ticket | Fixes |
|---|---|---|---|---|---|---|---|
| A1 | Record the rejection before cancelling the run in `markJobRejected` | prevent | Nanostack routine (echopoint-investigation) | 2026-11-19 | open | [echopoint-runner#PR](https://github.com/nanostack-dev/echopoint-runner/pulls) | R1 |
| A2 | Tag a release only after `Go Build and Test` passed on that main commit (or tag from the go.yml run) | prevent | echopoint-runner maintainers | 2026-12-03 | open |  | R2 |
| A3 | The flow routine compares the runner services' `app.commit` with runner main HEAD every run | detect | Nanostack routine (echopoint-investigation) | 2026-11-19 | done |  | R3 |
| A4 | Alert when the echopoint-runner main CI fails after a merge | detect | CI owners (infra / ci-workflows) | 2026-12-03 | open |  | R3 |
| A5 | Deployment runbook: after a merge, confirm the main run is green and the services log `main-<sha>` | process | Nanostack routine (echopoint-investigation) | 2026-11-19 | done |  | R3 |

## Responders

- **Investigation**: Nanostack routine (echopoint-investigation)

## Messaging

### Internal

Runner v0.112.0 (transport retries) was tagged but never deployed: main CI failed on a rejected-lease race. Fix records the rejection before cancelling; its merge deploys both.

## Supporting information

- [Failed main run](https://github.com/nanostack-dev/echopoint-runner/actions/runs/44573501171)
- [echopoint-runner#126](https://github.com/nanostack-dev/echopoint-runner/pull/126)
