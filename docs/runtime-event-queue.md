# Runtime Event Queue and Sim Time

## Purpose

This document describes the current runtime event queue implemented for Story `H2`.

The Go source of truth is:

- `internal/runtime/queue/manager.go`
- `internal/runtime/queue/manager_test.go`
- `internal/store/store.go`
- `internal/store/sqlite/store.go`
- `internal/store/sqlite/migrations/0004_event_queue.sql`

## Scope

The H2 slice provides deterministic scheduling for delayed simulator events.

Implemented operations:

- persist a per-session simulation clock
- schedule pending events for `push` or `pull` delivery
- advance session simulation time without sleeping or virtualizing process time
- deliver due `push` events when time advances
- deliver due `pull` events when the runtime asks for them

## State Model

`SessionClock` stores the current simulation time for one runtime session.

Fields:

- `session_name`
- `current_time`
- `updated_at`

`current_time` is exposed in Go and persisted as `session_clocks.sim_time` to avoid SQLite keyword ambiguity.

`ScheduledEvent` stores one delayed simulator event.

Fields:

- `event_id`
- `session_name`
- `service`
- `topic`
- `delivery_mode`
- `due_at`
- `payload`
- `status`
- `created_at`
- `delivered_at`

Current delivery modes:

- `push`: delivered automatically from `AdvanceTime`
- `pull`: delivered only from `PullDue`

Current statuses:

- `pending`
- `delivered`

## Time Semantics

The runtime uses non-virtualized simulation time:

- no goroutine sleeps until an event is due
- no process-wide clock is patched
- each session clock starts at wall time when first accessed
- `AdvanceTime(session, duration)` moves only that session's persisted simulation clock
- due events are selected with `due_at <= current_time`

This keeps replay deterministic while avoiding hidden effects on SDK or application clocks.

## Delivery Semantics

`Manager.AdvanceTime(session, duration)`:

- rejects negative durations
- initializes the session clock if needed
- persists the advanced clock
- finds pending due `push` events ordered by `due_at`, then `event_id`
- marks those events `delivered` in the store
- returns the delivered push events

`Manager.PullDue(session)`:

- initializes the session clock if needed
- finds pending due `pull` events ordered by `due_at`, then `event_id`
- marks those events `delivered`
- returns the delivered pull events

Both paths mark delivery in one store-level transaction for the event IDs returned by that call.

## Current Limits

- no CLI command exposes `advance_time` yet
- no simulator has been wired to enqueue events yet
- delivered events are retained; cancellation and tombstones are not implemented
- snapshots do not yet embed event queue state

## Validation Coverage

Current tests cover:

- SQLite migration tables and indexes
- session clock persistence
- scheduled event persistence and due-event lookup
- delivery status updates
- store-level session deletion of queue state
- push delivery through `AdvanceTime`
- pull delivery through `PullDue`
- negative `AdvanceTime` rejection
- missing-session error behavior
