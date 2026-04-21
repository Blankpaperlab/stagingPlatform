# Artifact Schema

## Purpose

This document defines the current Stagehand recording artifact contract for Story `B2`.

It covers:

- `Run`
- `Interaction`
- `Event`
- `ScrubReport`
- version metadata fields
- the meaning of `complete`, `incomplete`, and `corrupted` runs

Current artifact schema version: `v1alpha1`

## Design Boundary

This schema is the canonical in-repo contract for recorded artifacts.

It is intentionally separate from:

- config schema in `docs/config-schema.md`
- SQLite table layout, which can evolve independently
- future import/export compatibility policy, which remains Story `B3`

## Top-Level `Run`

`Run` is the persisted artifact for one agent execution.

Required top-level version fields:

- `schema_version`
- `sdk_version`
- `runtime_version`
- `scrub_policy_version`

Required identity and lifecycle fields:

- `run_id`
- `session_name`
- `mode`
- `status`
- `started_at`

Optional lifecycle and provenance fields:

- `ended_at`
- `base_snapshot_id`
- `agent_version`
- `git_sha`

Contained collections:

- `interactions`
- `integrity_issues`

## `Run.status`

Allowed values:

- `complete`
- `incomplete`
- `corrupted`

Semantics:

- `complete`: the run closed cleanly, passed structural validation, and is eligible for replay.
- `incomplete`: the run artifact is structurally valid enough to inspect, but capture did not finish cleanly. It must carry at least one `integrity_issue` and should not be replayed by default.
- `corrupted`: the run artifact is present for debugging, but integrity checks failed badly enough that replay must be rejected. It must carry at least one `integrity_issue`.

Explicit rule:

- `complete` runs must have `ended_at` and must not contain `integrity_issues`.

## `Interaction`

`Interaction` represents one ordered external operation inside a run.

Required fields:

- `run_id`
- `interaction_id`
- `sequence`
- `service`
- `operation`
- `protocol`
- `request`
- `events`
- `scrub_report`

Optional fields:

- `parent_interaction_id`
- `fallback_tier`
- `streaming`
- `extracted_entities`
- `latency_ms`

Validation rules:

- interaction `run_id` must match the parent run
- interaction `sequence` must be strictly increasing inside the run
- `fallback_tier` is optional, but when present it must be one of:
  - `exact`
  - `nearest_neighbor`
  - `state_synthesis`
  - `llm_synthesis`

Supported protocol values in `v1alpha1`:

- `http`
- `https`
- `sse`
- `websocket`
- `postgres`

## `Request`

`Request` is attached to each interaction and captures the outbound call shape.

Required fields:

- `url`
- `method`

Optional fields:

- `headers`
- `body`

Design note:

- request and response are not modeled as a pair
- response behavior is captured through the ordered `events` list

## `Event`

`Event` is the atomic ordered entry within an interaction.

Required fields:

- `sequence`
- `t_ms`
- `sim_t_ms`
- `type`

Optional fields:

- `data`
- `nested_interaction_id`

Validation rules:

- event `sequence` must be strictly increasing within an interaction
- `t_ms` must be non-negative and non-decreasing
- `sim_t_ms` must be non-negative and non-decreasing

Known event types currently declared in code:

- `request_sent`
- `response_received`
- `stream_chunk`
- `stream_end`
- `tool_call_start`
- `tool_call_end`
- `error`
- `timeout`

This list is the current known set, not a promise that no new event types will ever be added.

## `ScrubReport`

`ScrubReport` records what the scrubber did before persistence.

Required fields:

- `scrub_policy_version`
- `session_salt_id`

Optional fields:

- `redacted_paths`

Validation rules:

- interaction `scrub_report.scrub_policy_version` must match run `scrub_policy_version`
- `session_salt_id` is required even if the interaction has no redacted paths, because session-scoped deterministic replacement is part of the V1 scrub design

## `IntegrityIssue`

`IntegrityIssue` explains why a run is not `complete`.

Required fields:

- `code`
- `message`

Optional fields:

- `interaction_id`
- `event_sequence`

Known issue codes currently declared in code:

- `recorder_shutdown`
- `interrupted_write`
- `missing_end_state`
- `schema_validation_failed`

## Example JSON

```json
{
  "schema_version": "v1alpha1",
  "sdk_version": "0.1.0-alpha.0",
  "runtime_version": "0.1.0-alpha.0",
  "scrub_policy_version": "v1",
  "run_id": "run_abc123",
  "session_name": "onboarding-flow",
  "mode": "record",
  "status": "complete",
  "started_at": "2026-04-21T10:00:00Z",
  "ended_at": "2026-04-21T10:00:00.402Z",
  "git_sha": "abcdef123456",
  "interactions": [
    {
      "run_id": "run_abc123",
      "interaction_id": "int_47",
      "sequence": 47,
      "service": "openai",
      "operation": "chat.completions.create",
      "protocol": "https",
      "streaming": true,
      "request": {
        "url": "https://api.openai.com/v1/chat/completions",
        "method": "POST",
        "headers": {
          "content-type": ["application/json"]
        },
        "body": {
          "model": "gpt-5.4"
        }
      },
      "events": [
        { "sequence": 1, "t_ms": 0, "sim_t_ms": 0, "type": "request_sent" },
        {
          "sequence": 2,
          "t_ms": 234,
          "sim_t_ms": 234,
          "type": "stream_chunk",
          "data": { "delta": "Hello" }
        },
        {
          "sequence": 3,
          "t_ms": 341,
          "sim_t_ms": 341,
          "type": "tool_call_start",
          "nested_interaction_id": "int_48"
        },
        {
          "sequence": 4,
          "t_ms": 402,
          "sim_t_ms": 402,
          "type": "stream_end",
          "data": { "finish_reason": "tool_calls" }
        }
      ],
      "extracted_entities": [
        { "type": "assistant_message", "id": "msg_123", "attrs": { "role": "assistant" } }
      ],
      "scrub_report": {
        "redacted_paths": ["request.headers.authorization", "request.body.messages[0].content"],
        "scrub_policy_version": "v1",
        "session_salt_id": "salt_xyz"
      },
      "latency_ms": 402
    }
  ]
}
```

## Current Implementation

The schema and validation logic live in:

- `internal/recorder/schema.go`
- `internal/recorder/schema_test.go`

What is implemented now:

- Go structs for the artifact contract
- validation for required fields and ordering
- run-state rules for `complete`, `incomplete`, and `corrupted`
- JSON round-trip test coverage

What is intentionally deferred:

- formal compatibility and migration policy
- external JSON Schema file
- import/export CLI commands
