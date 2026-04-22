# Structural Scrub Pipeline

## Purpose

This document describes the current Stagehand structural scrub pipeline implemented for Story `E1`.

The Go source of truth is:

- `internal/scrub/pipeline.go`
- `internal/scrub/pipeline_test.go`

This is the Go pre-persistence scrub engine used by the safe recording path.

It now covers:

- structural path rules
- detector-driven free-text scanning for standard secrets and PII
- session-scoped deterministic replacement driven by encrypted session salts

It still does not cover:

- custom rule files loaded from config
- direct Python-side persistence wiring

## Scope

The current pipeline scrubs a `recorder.Interaction` before persistence.

Supported surfaces:

- request headers
- request query parameters
- request JSON-like body payloads
- event data payloads such as `events[*].data.body`

The pipeline writes a `ScrubReport` onto the scrubbed interaction.

## Rule Model

Each rule has:

- `pattern`
- `action`
- optional `name`

Supported actions:

- `drop`
- `mask`
- `hash`
- `preserve`

## Path Syntax

Rules match structural paths such as:

- `request.headers.authorization`
- `request.query.token`
- `request.body.customer.email`
- `request.body.messages[*].content`
- `events[*].data.body.customer.email`

Supported wildcard forms:

- `*` for any object field segment
- `[*]` for any array index

## Rule Resolution

When multiple rules match one concrete path:

1. the most specific rule wins
2. if specificity ties, the later rule in the rule list wins

This lets a narrow `preserve` rule override a broader wildcard mask rule.

## Actions

### `drop`

Removes the matched field entirely.

Examples:

- remove `request.headers.authorization`
- remove `request.query.token`
- remove `request.body.customer.ssn`

### `mask`

Masks scalar values while preserving rough shape.

Current implementation:

- strings shorter than or equal to 4 runes become all `*`
- longer strings keep the last 4 runes and mask the prefix

Examples:

- `alice@example.com` -> `*************.com`
- `secret-token` -> `*******oken`

### `hash`

Replaces matched scalar values with a deterministic session-scoped replacement:

- generic format: `hash_<hex>`
- email-like values: `user_<hex>@scrub.local`

Current implementation requires:

- a non-empty `HashSalt` in pipeline options
- a `SessionSaltID` recorded in the scrub report

The replacement helper now lives in `internal/scrub/session_salt`.

This is deterministic within one pipeline configuration and is the structural precursor to Story `E3`.

### `preserve`

Leaves the matched value untouched and stops recursive scrubbing below that path.

## Detector-Driven Scrubbing

When no structural rule matches a string field, the pipeline now runs the configured detector library and replaces matched substrings in place.

Current detector-backed replacement behavior:

- email values become deterministic email-shaped replacements such as `user_<hex>@scrub.local`
- JWTs, phone numbers, SSNs, credit cards, and API keys become deterministic `hash_<hex>` replacements
- repeated appearances of the same sensitive value within one session resolve to the same replacement

Detector-driven scrubbing is applied to:

- non-protected request headers
- query-parameter values
- string leaves in request bodies
- string leaves in event data

## Built-In Default Rules

`DefaultRules()` currently returns:

- `request.headers.authorization` -> `drop`
- `request.headers.cookie` -> `drop`

These are the minimum safe structural rules required before persisted capture is allowed.

## Custom Rule Merging

V1 now supports inline custom rules from `scrub.custom_rules`.

Current merge behavior:

- built-in default rules are always included first
- custom rules are appended after defaults
- custom rules may add narrower or broader org-specific patterns
- custom rules may not reuse the exact pattern of a built-in rule
- custom rules may not duplicate another custom rule's exact `name` or `pattern`

This keeps org-specific extension possible without silently weakening the default auth and cookie protections.

## Scrub Report

Every scrubbed interaction gets a `ScrubReport` with:

- `scrub_policy_version`
- `session_salt_id`
- concrete `redacted_paths`

`redacted_paths` records the concrete paths changed during scrubbing, not the wildcard rule patterns.

Example:

- rule: `request.body.messages[*].content`
- report paths:
  - `request.body.messages[0].content`
  - `request.body.messages[1].content`

## Current Limits

Known limits in the current E1 implementation:

- `mask` and `hash` are intended for scalar or string-like values, not complex objects
- query parameters are rebuilt through `net/url`, so scrubbed query ordering follows encoded output
- inline custom rules are supported, but external custom rule files are not wired yet
- the pipeline is implemented in Go and the current durable write path is the recorder-side persisted writer, not direct Python capture
- the final runtime master-key source for session-salt encryption is not wired yet

## Validation Coverage

Current tests cover:

- default auth/cookie header dropping
- query hashing
- request body masking
- event payload masking
- detector-driven scrubbing across headers, query params, bodies, and event strings
- wildcard path matching
- preserve-overrides-wildcard behavior
- deterministic hashing within one pipeline
- invalid hash configuration
- invalid attempts to mask complex objects
