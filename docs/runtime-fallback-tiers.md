# Runtime Fallback Tiers

## Purpose

This document describes the current fallback matching core implemented for Story `H3`.

The Go source of truth is:

- `internal/runtime/fallback/matcher.go`
- `internal/runtime/fallback/matcher_test.go`
- `internal/config/config.go`
- `internal/store/sqlite/store.go`

## Scope

The H3 slice implements runtime fallback tiers `0-2`:

- tier 0: `exact`
- tier 1: `nearest_neighbor`
- tier 2: `state_synthesis`

Tier 3 `llm_synthesis` remains intentionally unsupported in the runtime matcher. Config validation can describe it as a future explicit option, but the H3 matcher rejects it so replay cannot silently drift into LLM-generated behavior.

## Matching Order

The matcher always evaluates tiers in deterministic order:

1. `exact`
2. `nearest_neighbor`
3. `state_synthesis`

The configured `fallback.allowed_tiers` controls which tiers are available. Disabled tiers are skipped, not attempted.

## Tier 0: Exact

Exact matching requires the same canonical request shape:

- service
- operation
- protocol
- HTTP method
- normalized URL with sorted query parameters
- canonical JSON request body

Headers are intentionally not part of the exact key because auth, cookie, and date-like headers are commonly scrubbed or unstable. Header similarity is still considered by nearest-neighbor matching.

When exact matching succeeds, the returned interaction has:

- `fallback_tier = "exact"`

## Tier 1: Nearest Neighbor

Nearest-neighbor matching first narrows candidates to the same:

- service
- operation
- protocol
- HTTP method
- URL host and path

It then scores:

- flattened JSON body similarity
- query parameter similarity
- non-sensitive header similarity

Sensitive headers such as `authorization`, `cookie`, and `set-cookie` are ignored. Candidate ties are resolved by recorded sequence and then interaction ID.

When nearest-neighbor matching succeeds, the returned interaction has:

- `fallback_tier = "nearest_neighbor"`

## Tier 2: State Synthesis

State synthesis is represented as an explicit hook:

- callers provide a synthesizer implementation
- the matcher passes the requested interaction and compatible recorded candidates
- the synthesizer returns a concrete interaction

When the hook succeeds, the returned interaction has:

- `fallback_tier = "state_synthesis"`

This hook is deliberately narrow. H3 defines the runtime boundary; later simulator stories are responsible for providing service-specific synthesis logic.

## Persistence

Fallback tier usage is stored on `recorder.Interaction.FallbackTier`.

The SQLite store already persists this field as:

- `interactions.fallback_tier`

H3 adds direct regression coverage to ensure the fallback tier round-trips through SQLite.

## Current Limits

- the CLI replay path still uses SDK exact replay for live child processes
- H3 adds the Go runtime matching core used by later simulator and generic HTTP work
- state synthesis is hook-based, not a built-in simulator
- LLM synthesis is rejected by the matcher

## Validation Coverage

Current tests cover:

- exact request matching
- nearest-neighbor matching for request variation
- `fallback.allowed_tiers` enforcement
- state synthesis hook invocation
- explicit rejection of `llm_synthesis`
- SQLite persistence of `fallback_tier`
