# Session-Scoped Deterministic Hashing

## Purpose

This document describes the current Stagehand session-salt and deterministic replacement behavior implemented for Story `E3`.

The Go source of truth is:

- `internal/scrub/session_salt/manager.go`
- `internal/scrub/session_salt/manager_test.go`
- `internal/scrub/pipeline.go`
- `internal/scrub/pipeline_test.go`

## Scope

The current implementation covers:

- per-session salt generation
- encryption of salts before persistence
- deterministic replacement helpers for hashed values
- reuse of the same replacement within one session
- separation of replacements across different sessions
- use of the session-salt manager by the recorder-side safe persisted writer

It does not yet:

- wire the session-salt manager into the Python persisted recording path
- load the master encryption key from final CLI or config plumbing
- add custom replacement formats from user config

## Session Salt Lifecycle

The current session-salt manager lives in `internal/scrub/session_salt`.

Current behavior:

1. `GetOrCreate(session)` checks the store for an existing session salt.
2. If none exists, it generates a fresh 32-byte salt and a new `salt_id`.
3. The salt is encrypted before persistence.
4. Later calls for the same session reuse the stored salt.

Current material model:

- `session_name`
- `salt_id`
- plaintext 32-byte salt in memory only
- `created_at`

## Encryption At Rest

Salt material is encrypted with AES-GCM before storage.

Current encryption behavior:

- 32-byte master key required
- random nonce per encrypted salt record
- ciphertext stored as a versioned JSON envelope
- additional authenticated data binds the ciphertext to:
  - `session_name`
  - `salt_id`
  - envelope version

This means a stored ciphertext cannot be moved to another session or salt ID without failing authentication.

## Deterministic Replacement

The current replacement helper is `session_salt.Replacement(salt, value)`.

Current behavior:

- uses HMAC-SHA256 with the session salt
- same `(salt, value)` pair always produces the same replacement
- different salts produce different replacements for the same input

Current output shape:

- email-like values become `user_<token>@scrub.local`
- other values become `hash_<token>`

This is intentionally simple, but it preserves replay-critical equality within a session.

## Replay-Fidelity Behavior

The key replay property now covered is identifier parity inside one session.

Example:

- create request uses `customer.email = alice@example.com`
- lookup request later uses `?email=alice@example.com`
- after scrubbing within the same session, both become the same deterministic replacement

That keeps scrubbed identifiers joinable during replay and later state lookups.

## Validation Coverage

Current tests cover:

- manager rejects invalid master-key length
- new session salts are generated and stored encrypted
- repeated `GetOrCreate()` for one session reuses the same salt
- deterministic replacement is stable within a session
- deterministic replacement differs across sessions
- scrubbed body and query identifiers stay aligned within one session for replay parity

## Current Limits

Known limits in the current E3 implementation:

- the master encryption key is a caller-provided byte slice, not final runtime config
- email shaping is the only format-aware replacement currently implemented
- encrypted salt storage exists in Go local mode only
- the Python SDK still emits placeholder scrub provenance until the persisted scrub path lands
