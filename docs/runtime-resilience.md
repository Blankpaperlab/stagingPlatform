# Runtime Resilience Behavior

## Purpose

This document describes the current runtime resilience behavior implemented for Story `H4`.

The Go source of truth is:

- `cmd/stagehand/main.go`
- `cmd/stagehand/workflow.go`
- `cmd/stagehand/main_test.go`
- `internal/store/sqlite/store.go`

## Scope

The H4 slice makes partial failures observable and prevents malformed imported interactions from being accepted silently.

Implemented behavior:

- run finalization records `complete`, `incomplete`, or `corrupted`
- managed command failures are marked `incomplete`
- malformed or missing capture bundles are marked `corrupted`
- imported interactions are preflight validated before persistence starts
- per-interaction SQLite writes remain atomic
- integrity issues include concrete issue codes and actionable messages

## Run Finalization

`finalizeRun` always moves a `running` run to a terminal lifecycle status.

Current terminal mapping:

- `complete`: command finished, capture/import succeeded, and all required replay output was produced
- `incomplete`: the managed user command failed or exited before a clean runtime finish
- `corrupted`: Stagehand could not trust the imported capture, validation failed, or persistence failed

Terminal failures are recorded as `recorder.IntegrityIssue` values on the run record.

Current issue-code mapping:

- `recorder_shutdown`: managed command/runtime process failure
- `missing_end_state`: expected capture or replay output was missing
- `schema_validation_failed`: imported interactions failed structural validation before persistence
- `interrupted_write`: persistence failed after preflight validation

## Import Validation

Capture bundles are normalized to the target run ID and deterministic interaction sequence order before validation.

Before any interaction is persisted:

- every imported interaction is validated structurally
- events must end in a terminal event
- parent/run IDs are normalized
- validation failures abort the entire import

This prevents a bundle with one valid interaction followed by one malformed interaction from partially landing in SQLite.

## Atomicity

SQLite still uses `WriteInteraction` as the explicit transactional boundary.

Guarantees:

- each interaction row and its events are written in one transaction
- validation failures before persistence write no interactions
- persistence failures after preflight are surfaced as `interrupted_write`

The local store does not yet provide a whole-run transaction across all interactions; H4 makes that limitation observable rather than silent.

## Current Limits

- replay subprocess failures are still controlled by the SDK/child-process behavior
- whole-run multi-interaction atomicity is not implemented
- recovery/retry policy is not implemented

## Validation Coverage

Current tests cover:

- complete record runs
- complete empty record runs
- managed command failure marked `incomplete`
- invalid imported bundle marked `corrupted`
- invalid imported bundle writes zero interactions
- explicit runtime failure classification in `finalizeRun`
- SQLite per-interaction atomicity
