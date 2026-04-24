# Runtime Session Lifecycle

## Purpose

This document describes the current runtime session lifecycle implemented for Story `H1`.

The Go source of truth is:

- `internal/runtime/session/manager.go`
- `internal/runtime/session/manager_test.go`
- `internal/store/store.go`
- `internal/store/sqlite/store.go`
- `internal/store/sqlite/migrations/0003_session_lifecycle.sql`

## Scope

The H1 slice provides the minimum persistent runtime model needed before stateful simulators and fallback matching are added. Event queue state is implemented separately in Story `H2`.

Implemented operations:

- create a named session
- snapshot session state
- restore a session to a prior snapshot
- fork a child session from a parent's current snapshot
- destroy a session and its dependent local data

## State Model

`SessionRecord` is the durable session identity and lifecycle row.

Fields:

- `session_name`
- `parent_session_name`
- `current_snapshot_id`
- `status`
- `created_at`
- `updated_at`

Current status values:

- `active`

Destroyed sessions are hard-deleted in local mode rather than tombstoned.

`SessionSnapshot` stores one immutable runtime state checkpoint.

Fields:

- `snapshot_id`
- `session_name`
- `parent_snapshot_id`
- `source_run_id`
- `state`
- `created_at`

The snapshot `state` is a JSON object. It is intentionally generic so later simulators can store service-scoped state without changing the H1 API.

## Operation Semantics

### Create

`Manager.Create(session)` inserts an active session with no current snapshot.

### Snapshot

`Manager.Snapshot(session, state)`:

- stores a JSON-cloned state object
- links the new snapshot to the previous current snapshot when one exists
- updates the session's `current_snapshot_id`

### Restore

`Manager.Restore(session, snapshot_id)`:

- rejects cross-session restore attempts
- updates the session's `current_snapshot_id` to the requested snapshot
- does not mutate historical snapshots

### Fork

`Manager.Fork(parent, child)`:

- creates a child session with `parent_session_name` set
- copies the parent's current snapshot into a new child snapshot when the parent has one
- links the child snapshot back to the parent snapshot through `parent_snapshot_id`
- keeps later child snapshots isolated from the parent session

### Destroy

`Manager.Destroy(session)` delegates to store-level `DeleteSession`.

Local destroy removes:

- runtime session row
- session snapshots
- runs for the session
- interactions and events through run cascade
- baselines through run cascade
- session scrub salt
- session event queue state

## Current Limits

- no CLI commands expose session lifecycle directly yet
- event queue state is persisted separately and is not embedded in snapshots yet
- no simulator-specific state schema exists yet
- local mode hard-deletes destroyed sessions instead of preserving tombstones

## Validation Coverage

Current tests cover:

- create, snapshot, restore, and destroy
- forked child snapshots linked to parent snapshots
- parent/child session isolation after fork
- cross-session restore rejection
- SQLite migration tables and indexes
- store-level deletion of runtime session state without deleting unrelated sessions
