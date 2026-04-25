# SQLite Local Store

## Purpose

This document defines the current SQLite-backed local store for Stagehand Plan A local mode.

The Go source of truth is:

- `internal/recording/writer.go`
- `internal/store/store.go`
- `internal/store/sqlite/migrate.go`
- `internal/store/sqlite/store.go`
- `internal/store/sqlite/migrations/0001_initial.sql`
- `internal/store/sqlite/migrations/0002_add_run_integrity_issues.sql`
- `internal/store/sqlite/migrations/0003_session_lifecycle.sql`
- `internal/store/sqlite/migrations/0004_event_queue.sql`
- `internal/store/sqlite/migrations/0005_add_run_metadata.sql`

## Scope

The local SQLite store now covers:

- schema and migration bootstrapping from Story `C1`
- the first stable persistence interface from Story `C2`
- reusable storage contract coverage from Story `C3`

Current store responsibilities:

- create, read, update, and delete run records
- write and read interactions and ordered events
- persist session scrub salts
- write and look up baselines
- persist runtime session records and session snapshots
- persist session simulation clocks and scheduled runtime events
- delete whole local sessions
- enforce an atomic write boundary per interaction

This is the local-only storage layer for the first safe record/replay slice. It now includes the first runtime session/snapshot state model plus persisted event queue state, but assertion execution and CLI-level retention workflows remain future work.

## Safe Persisted Recording Path

The standard persisted-recording path now runs through `internal/recording.Writer`.

Current behavior:

- loads the parent run record to get the session name and expected scrub policy
- loads or creates the encrypted session salt through `internal/scrub/session_salt.Manager`
- builds the Go scrub pipeline with structural rules plus detector-driven free-text scrubbing
- scrubs the interaction before it ever reaches `ArtifactStore.WriteInteraction`
- persists only the scrubbed interaction payload

This is the path that makes the Epic `E` checkbox honest:

- standard secrets are not persisted in plaintext in the normal local durable recording flow

The low-level store remains intentionally dumb. It persists what it is given. The safety guarantee now lives in the recorder-side persisted writer, not in hidden SQLite-side mutation.

Current lifecycle model:

- the store status field is a lifecycle state, not just an artifact state
- supported lifecycle values are `running`, `complete`, `incomplete`, and `corrupted`
- `running` runs can be created and updated while interactions are still being written
- terminal artifact hydration through `GetRun` is intentionally limited to `complete`, `incomplete`, and `corrupted`
- callers that need in-progress metadata should use the run-record path instead of artifact hydration

## Store Interface

The shared store contract lives in `internal/store/store.go`.

Current exported models:

- `ArtifactStore`
- `RunRecord`
- `ScrubSalt`
- `Baseline`
- `SessionRecord`
- `SessionSnapshot`
- `SessionClock`
- `ScheduledEvent`
- `ErrNotFound`

The current SQLite implementation is `internal/store/sqlite.Store`.

Current behavior:

- `CreateRun` validates and inserts a top-level run record
- `GetRunRecord` loads top-level run metadata, including `running` runs
- `GetLatestRunRecord` loads the latest stored run record for a session regardless of lifecycle state
- `GetRun` hydrates a terminal artifact run plus all ordered interactions and events
- `GetLatestRun` returns the latest replayable `complete` run for a session, not the latest corrupted or incomplete run
- `UpdateRun` updates the top-level run lifecycle fields
- `DeleteRun` removes one run and its dependent rows while preserving the session scrub salt
- `DeleteSession` removes all runs for a session plus the session scrub salt
- `WriteInteraction` validates the interaction against the parent run and writes the interaction plus events in one transaction
- `ListInteractions` returns ordered interactions for a run, each with ordered events
- `PutScrubSalt` and `GetScrubSalt` persist session-scoped scrub salt material
- `PutBaseline`, `GetBaseline`, and `GetLatestBaseline` support baseline storage and lookup, but baselines require a `complete` source run and a matching session name
- `CreateSession`, `GetSession`, `UpdateSession`, `PutSessionSnapshot`, `GetSessionSnapshot`, and `GetLatestSessionSnapshot` support the runtime session lifecycle
- `PutSessionClock` and `GetSessionClock` persist per-session non-virtualized simulation time
- `PutScheduledEvent`, `GetScheduledEvent`, `ListDueScheduledEvents`, and `MarkScheduledEventsDelivered` support delayed runtime events for push and pull delivery modes
- missing records are mapped to `store.ErrNotFound`

## Migration Strategy

Stagehand uses embedded, ordered SQL migrations.

Current behavior:

1. open the SQLite database
2. create `schema_migrations` if it does not exist
3. apply unapplied `migrations/*.sql` files in lexical order
4. record each applied migration in `schema_migrations`

Operational assumption:

- SQLite foreign-key enforcement is enabled with `PRAGMA foreign_keys = ON`
- that setting is per connection, not global to the database file
- the local store intentionally uses one open/idle connection in V1 so cascade behavior stays stable
- if connection pooling is widened later, the pragma setup must be revisited explicitly rather than assumed

Migrations are idempotent at the runner level because already-applied versions are skipped.

Current migrations:

- `0001_initial.sql`
  Creates the base local tables and indexes.
- `0002_add_run_integrity_issues.sql`
  Adds `integrity_issues_json` to `runs` so run status and corruption state can be persisted without losing schema fidelity.
- `0003_session_lifecycle.sql`
  Adds `sessions` and `session_snapshots` for runtime session create, fork, snapshot, restore, and destroy behavior.
- `0004_event_queue.sql`
  Adds `session_clocks` and `scheduled_events` for runtime event scheduling and deterministic simulation-time delivery.
- `0005_add_run_metadata.sql`
  Adds `metadata_json` to `runs` for runtime provenance such as applied error-injection rules.

## Tables

### `runs`

Stores top-level run lifecycle and version metadata.

Key fields:

- `run_id`
- `session_name`
- `mode`
- `status`
- `schema_version`
- `sdk_version`
- `runtime_version`
- `scrub_policy_version`
- `base_snapshot_id`
- `agent_version`
- `git_sha`
- `started_at`
- `ended_at`
- `integrity_issues_json`
- `metadata_json`

Status semantics:

- `running` means capture or replay work has started but no terminal artifact has been finalized yet
- `complete`, `incomplete`, and `corrupted` are terminal states and map to recorder artifact statuses

### `interactions`

Stores ordered external interactions per run.

Key fields:

- `interaction_id`
- `run_id`
- `parent_interaction_id`
- `sequence`
- `service`
- `operation`
- `protocol`
- `streaming`
- `fallback_tier`
- `request_json`
- `scrub_report_json`
- `extracted_entities_json`
- `latency_ms`

### `events`

Stores ordered event records inside an interaction.

Key fields:

- `event_id`
- `interaction_id`
- `sequence`
- `t_ms`
- `sim_t_ms`
- `type`
- `data_json`
- `nested_interaction_id`

### `assertions`

Stores assertion execution results per run.

Key fields:

- `assertion_id`
- `run_id`
- `definition_json`
- `result`
- `failure_reason`
- `evidence_json`

### `baselines`

Stores baseline selections for session-level regression comparison.

Key fields:

- `baseline_id`
- `session_name`
- `source_run_id`
- `git_sha`
- `created_at`

### `scrub_salts`

Stores encrypted scrub salt material keyed by local session name.

Key fields:

- `session_name`
- `salt_id`
- `salt_encrypted`
- `created_at`

Current salt payload behavior:

- `salt_encrypted` stores a versioned AES-GCM envelope, not plaintext salt bytes
- encryption is authenticated against `session_name` and `salt_id`
- plaintext salt material exists only in memory after decryption

### `sessions`

Stores runtime session identity and current snapshot pointers.

Key fields:

- `session_name`
- `parent_session_name`
- `current_snapshot_id`
- `status`
- `created_at`
- `updated_at`

### `session_snapshots`

Stores immutable JSON runtime state checkpoints.

Key fields:

- `snapshot_id`
- `session_name`
- `parent_snapshot_id`
- `source_run_id`
- `state_json`
- `created_at`

### `session_clocks`

Stores one non-virtualized simulation clock per runtime session.

Key fields:

- `session_name`
- `sim_time`
- `updated_at`

The Go model exposes this as `SessionClock.CurrentTime`; the SQLite column is named `sim_time` to avoid ambiguity with SQLite's `current_time` keyword.

### `scheduled_events`

Stores delayed simulator events for push or pull delivery.

Key fields:

- `event_id`
- `session_name`
- `service`
- `topic`
- `delivery_mode`
- `due_at`
- `payload_json`
- `status`
- `created_at`
- `delivered_at`

Current delivery modes are `push` and `pull`. Current statuses are `pending` and `delivered`.

## Indexes

The current schema adds indexes for the main local lookup paths:

- run lookup by session and start time
- run lookup by status and start time
- interaction lookup by run
- interaction ordering by `(run_id, sequence)`
- event lookup by interaction
- event ordering by `(interaction_id, sequence)`
- assertion lookup by run
- baseline lookup by session and creation time
- scrub salt lookup by `salt_id`
- session lookup by parent session
- session snapshot lookup by session and creation time
- session snapshot ancestry lookup
- session clock update lookup
- scheduled event lookup by `(session_name, status, delivery_mode, due_at, event_id)`
- scheduled event creation lookup by session

## Atomicity Rules

`WriteInteraction` is the current explicit transactional boundary.

Current guarantees:

- the interaction row and all of its event rows are written in one SQLite transaction
- existing event rows for the same interaction are replaced inside that transaction
- a failure during event insertion rolls the whole interaction write back
- invalid interaction payloads are rejected before persistence
- interactions can be written while the parent run is still `running`

This is the first concrete protection against partially written interaction artifacts.

The current CLI `record` and `replay` commands use this store together with `internal/recording.Writer`:

- `record` persists scrubbed managed-subprocess captures into a local run
- `replay` persists the managed-subprocess replay result as a new replay-mode run
- `inspect` reads run records plus ordered interactions directly so failed runs can be debugged without artifact hydration

## Deletion Semantics

Current local deletion behavior is defined at the store layer.

### `DeleteRun`

- deletes the target run row
- cascades deletion to dependent interactions and events
- removes baselines that reference that run
- preserves the session scrub salt because scrub salts are session-scoped, not run-scoped
- returns `store.ErrNotFound` if the run does not exist

### `DeleteSession`

- deletes all runs for the target session, including their dependent rows
- removes baselines indirectly through the `baselines.source_run_id -> runs.run_id` `ON DELETE CASCADE`
- deletes the session scrub salt
- deletes the runtime session row and its snapshots
- deletes session clocks and scheduled events
- returns `store.ErrNotFound` if the session has no stored runs, runtime session row, snapshots, queue state, or scrub salt

These semantics are now enforced by reusable store contract tests so future backends can match them.

## Rollback Policy

Down migrations are an explicit non-goal for V1 local mode.

Current policy:

- migrations are forward-only
- local rollback is handled by deleting the local database or restoring from a backup copy
- no `down.sql` migration chain exists yet

This is intentional for the first local store slice. The migration surface is still small, and forward-only behavior is simpler and more reliable than pretending bidirectional migration support exists.
