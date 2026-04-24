# Work Completed and Current State

## Purpose

This document records what has actually been completed in the repository so far.

It is not a roadmap. It is a current-state report.

## Completed Work

## 1. Monorepo Scaffold

Completed stories:

- Story A1: Create monorepo skeleton

What was done:

- created the root repo layout for Go, Python, TypeScript, dashboard placeholder, docs, examples, and GitHub workflows
- initialized the Go module
- created Python and TypeScript package skeletons
- added root repo config files such as `.gitignore`, `.editorconfig`, and Prettier config

Current outcome:

- the repo structure is stable enough to begin implementation without moving directories around

## 2. Baseline CI Automation

Completed stories:

- Story A2: Add baseline CI automation

What was done:

- added GitHub Actions CI workflow
- added root task runner commands in `package.json`
- added Python smoke tests
- added TypeScript smoke tests
- added repo-level formatting checks

Current outcome:

- PRs can run format, build, lint, and test flows through a common entrypoint

## 3. Packaging Shells

Completed stories:

- Story A3: Create packaging shells

What was done:

- added Go build targets
- added Python packaging metadata
- added TypeScript package metadata and exports
- added version placeholder files and constants
- documented local build commands

Current outcome:

- the repo is structurally ready for later packaging and publishing work

## 4. Go Build and Test Enforcement

Completed work:

- fixed the critical gap where Go code existed but had not been locally built or tested

What was done:

- installed the Go toolchain locally
- refactored Go command binaries to be testable
- added real Go unit tests
- updated CI to explicitly build Go binaries
- updated root task runner to enforce Go build and test in `ci:all`

Current outcome:

- Go code is no longer decorative
- local and CI workflows now enforce Go formatting, build, vet, and tests

Validated commands:

- `go build ./cmd/stagehand ./cmd/stagehandd`
- `go vet ./...`
- `go test -cover ./...`
- `npm run ci:all`

## 5. Config Schema Contract

Completed stories:

- Story B1: Define config schemas

What was done:

- implemented runtime config schema in Go
- implemented test-run config schema in Go
- added default values
- added aggregated validation errors
- added YAML loading with strict field decoding
- added sample `stagehand.yml`
- added sample `stagehand.test.yml`
- added config reference documentation
- added Go tests for valid and invalid config cases

Current outcome:

- config is now a real contract, not just planning text

Validated commands:

- `go test ./internal/config -v`
- `go test -cover ./...`
- `npm run ci:all`

## 6. Recording Artifact Schema Contract

Completed stories:

- Story B2: Define run and event artifact schemas

What was done:

- implemented the canonical Go schema for `Run`, `Interaction`, `Event`, `ScrubReport`, and integrity issues
- added explicit artifact version fields: `schema_version`, `sdk_version`, `runtime_version`, and `scrub_policy_version`
- defined run states for `complete`, `incomplete`, and `corrupted`
- added artifact validation rules for ordering, required fields, scrub provenance, and replay eligibility
- added artifact schema reference documentation
- added Go tests covering valid artifacts, invalid state combinations, ordering failures, and JSON round-trip behavior

Current outcome:

- recording artifacts now have an explicit contract instead of being implied by planning documents
- later storage, SDK, and replay code can build against a stable recorder model

Validated commands:

- `go test ./internal/recorder -v`
- `go test -cover ./...`
- `npm run ci:all`

## 7. Schema Validation Fixtures and Compatibility Policy

Completed stories:

- Story B3: Define validation and compatibility rules

What was done:

- chose a code-first validation strategy instead of external schema files as the primary validator
- added fixture-based schema tests for config and artifact validation
- added strict artifact loading with unknown-field rejection
- documented current alpha exact-match behavior for schema versions
- documented minor-version compatibility rules, major-version migration expectations, and no-bump change rules

Current outcome:

- schema validation behavior is now fixture-backed instead of relying only on inline tests
- compatibility promises are explicit and intentionally conservative
- the repo now documents what is and is not allowed when schema contracts change

Validated commands:

- `go test ./internal/config -v`
- `go test ./internal/recorder -v`
- `go test -cover ./...`
- `npm run ci:all`

## 8. SQLite Schema and Migration Runner

Completed stories:

- Story C1: Define SQLite schema and migrations

What was done:

- added the initial embedded SQLite migration for local mode
- created tables for runs, interactions, events, assertions, baselines, and scrub salts
- added indexes for run lookup and interaction/event ordering
- implemented a migration runner with a `schema_migrations` metadata table
- documented forward-only migration behavior and the explicit non-goal of down migrations in V1
- added SQLite migration tests for fresh setup and idempotent reruns

Current outcome:

- the repo now has a real local storage bootstrap path instead of a placeholder store directory
- later store CRUD work can build on an actual migrated schema

Validated commands:

- `go test ./internal/store/sqlite -v`
- `go test -cover ./...`
- `npm run ci:all`

## 9. SQLite Store Interface

Completed stories:

- Story C2: Implement store interface

What was done:

- added a shared store contract in `internal/store/store.go`
- added a SQLite-backed store implementation in `internal/store/sqlite/store.go`
- implemented create, read, and update operations for top-level runs
- implemented write and read paths for interactions and ordered events
- implemented scrub salt persistence and retrieval
- implemented baseline write, lookup, and latest-baseline selection paths
- added an explicit transaction boundary for interaction plus event writes
- added explicit support for `running` run lifecycle state in the store layer
- added validation so invalid interactions are rejected before persistence
- added Go tests for CRUD behavior, atomic rollback, not-found mapping, and baseline/salt lookup behavior

Current outcome:

- recorder and later runtime code now have a concrete local persistence interface to build against
- the local store no longer stops at migrations; it can now hydrate and persist the main artifact shapes used by the first record/replay slice
- the store can persist in-progress runs honestly instead of pretending every run is already a terminal artifact

## 10. Storage Contract Tests

Completed stories:

- Story C3: Add storage contract tests

What was done:

- added a reusable backend-agnostic store contract suite under `internal/store/contracttest`
- defined contract coverage for lifecycle transitions, interaction ordering, incomplete/corrupted terminal runs, and deletion behavior
- added SQLite execution of the shared contract suite
- pulled local deletion semantics into the store interface with `DeleteRun` and `DeleteSession`
- documented concrete local deletion rules so future backends have a target behavior to match

Current outcome:

- store behavior is now specified by executable contract tests instead of only SQLite-specific examples
- Postgres can later be validated against the same suite rather than inventing new behavior
- local deletion behavior is now defined at the storage layer instead of being deferred prose

## 11. Python Bootstrap

Completed stories:

- Story D1: Implement Python bootstrap

What was done:

- replaced the placeholder Python `init()` with a real bootstrap entrypoint
- added explicit mode handling for `record`, `replay`, and `passthrough`
- added a process-local runtime singleton with double-initialization protection
- generated per-init run metadata including `run_id`, session, mode, and version data
- added runtime access helpers so later recorder/interception code can read bootstrap state
- added optional config-path resolution with explicit path support and default `stagehand.yml` autodiscovery
- added Python tests covering valid bootstrap, invalid modes, empty sessions, config resolution, runtime access, and double init rejection

Current outcome:

- the Python SDK is no longer a pure scaffold
- later interception and recorder hooks now have a real bootstrap/runtime surface to build on
- one-line initialization works for the first time in the Python package

## 12. Python `httpx` Interception

Completed stories:

- Story D2: Add HTTP interception

What was done:

- added sync and async `httpx` interception at the client `send` layer
- added a Python-side normalized interaction model aligned to the core artifact shape
- added an in-memory capture buffer owned by the runtime singleton
- captured request metadata, latency, response metadata, and response bodies where safely available
- captured timeout and generic request failures as explicit events instead of dropping them
- added Python tests for sync, async, timeout, error, and artifact-shaped export behavior

Current outcome:

- Python agent traffic through `httpx` can now be captured without changing user call sites
- the runtime exposes normalized interactions for later recorder persistence work
- the current D2 output is intentionally pre-scrub and in-memory only; it is not yet the persisted recording path

## 13. OpenAI-Aware Capture and Exact Replay

Completed stories:

- Story D3: Add OpenAI-aware capture and replay hooks

What was done:

- added OpenAI request-shape detection for chat-completions create calls
- upgraded streaming capture to parse OpenAI SSE blocks in order
- preserved tool-call boundaries as explicit `tool_call_start` and `tool_call_end` events
- added seeded exact replay for OpenAI requests in Python replay mode
- routed replayed responses back through the same `httpx` `send` and `stream` calling surface
- added a concrete replay-miss error for unseeded OpenAI replay calls
- added a minimal example onboarding agent and integration test that proves offline replay

Current outcome:

- the first demo loop now exists for Python and OpenAI: capture once, seed replay, rerun offline through the same client API
- exact replay currently depends on in-memory seeded interactions, not persisted stored artifacts
- this is enough to prove the wedge before recorder persistence and scrubbing are wired into the same path

## 14. Structural Scrub Pipeline

Completed stories:

- Story E1: Build structural scrub pipeline

What was done:

- added a Go scrub package under `internal/scrub`
- implemented path-based structural rule matching with exact and wildcard support
- implemented `drop`, `mask`, `hash`, and `preserve` actions
- added default request-header rules for `authorization` and `cookie`
- scrubbed request headers, query parameters, request bodies, and event payloads
- generated concrete `ScrubReport.redacted_paths` entries from applied rules
- added tests for wildcard matching, preserve overrides, deterministic hashing, and invalid complex-object masking

Current outcome:

- Stagehand now has a real pre-persistence structural scrubber instead of only a scrub-report schema
- the minimum security boundary for dropping auth and cookie headers is implemented in Go
- detector-driven mutation and config-loaded custom rules now build on top of this base pipeline

## 15. Detector Library

Completed stories:

- Story E2: Build detector library

What was done:

- added a dedicated Go detector package under `internal/scrub/detectors`
- added detectors for email, JWT, phone numbers, SSNs, Luhn-validated credit cards, and common API-key prefixes
- added semantic validation so JWT and card matches are not purely regex-based
- added ordered match output with deduplication across detector overlaps
- added a corpus file with positive and negative cases for every detector kind
- added Go tests for corpus coverage, match ordering, and duplicate suppression

Current outcome:

- Stagehand now has a reusable free-text detector layer for shape-independent secrets and PII
- the detector package is now wired into the Go scrub pipeline for automatic payload mutation
- Python durable capture still needs to route through the recorder-side persisted writer

## 16. Session-Scoped Deterministic Hashing

Completed stories:

- Story E3: Add session-scoped deterministic hashing

What was done:

- added a Go session-salt manager under `internal/scrub/session_salt`
- added per-session 32-byte salt generation
- added AES-GCM encryption before salt persistence in the local store
- added deterministic replacement helpers used by hash-based scrubbing
- made email-like hashed values replay-safe by keeping an email-shaped replacement
- added tests proving same-session stability, cross-session separation, and replay identifier parity

Current outcome:

- Stagehand now has a real session-scoped hashing model instead of plain `sha256(salt || value)` helpers
- stored scrub salts are encrypted at rest in local mode
- the scrub pipeline can now preserve equality of scrubbed identifiers within one session, which is necessary for replay fidelity

## 17. User-Configurable Scrub Rules

Completed stories:

- Story E4: Add user-configurable scrub rules

What was done:

- added inline `scrub.custom_rules` to the runtime config schema
- added merge logic so custom rules are appended after built-in defaults
- rejected custom rules that try to reuse built-in exact patterns
- rejected duplicate custom-rule names and patterns
- added config tests, fixture tests, and scrub merge tests
- documented custom rule examples in the config and scrub docs

Current outcome:

- teams can now add org-specific structural scrub rules without editing code
- built-in auth and cookie protections remain non-overridable in V1
- external custom rule files remain deferred; inline config is the supported path

## 18. Safe Persisted Recording Path

Completed work:

- closed the Epic `E` gap for plaintext secret persistence in the standard local durable recording path

What was done:

- added `internal/recording.Writer` as the recorder-side persisted write path
- made the persisted writer require scrub to stay enabled for V1 durable recording
- made the runtime config reject disabling standard detectors in V1
- wired structural rules, detector-driven mutation, encrypted session salts, and store writes into one path
- added end-to-end tests that inspect raw SQLite JSON rows directly and prove standard secrets are not persisted in plaintext

Current outcome:

- the normal Go durable recording path now scrubs before SQLite persistence instead of relying on callers to remember to do it
- standard secrets such as emails, JWTs, phone numbers, SSNs, cards, API keys, auth headers, and cookies are no longer written to SQLite in plaintext through that path
- the low-level store remains intentionally dumb, but the recorder-side persisted writer now owns the safety guarantee

## 19. CLI `record` Managed Subprocess Flow

Completed stories:

- Story F1: Implement `record`

What was done:

- replaced the placeholder CLI scaffold with a real subcommand entrypoint for `stagehand record`
- added root help text and record-specific help text
- added `--session` and `--config` support
- changed the command contract so `record` requires a managed command after `--`
- resolved the runtime config and local SQLite path from the configured storage directory
- created a `running` run record in storage before subprocess execution
- launched a managed child process with Stagehand environment wiring and a capture-bundle output path
- loaded the exported interaction bundle from disk and persisted it through `internal/recording.Writer`
- finalized the run to `complete` or `corrupted` depending on subprocess and persistence outcome
- added tests for help output, required flag validation, missing-command rejection, subprocess capture import, scrubbed persistence, and JSON result output

Current outcome:

- the Go CLI now performs real work instead of just creating run metadata
- users can run a Stagehand-aware child process, capture interactions, scrub them, and persist a real recording run without touching SQLite directly
- the current boundary is explicit: the child process must export a capture bundle through the SDK; proxy-driven transparent capture is still future work

## 20. CLI `replay` Managed Exact-Replay Flow

Completed stories:

- Story F2: Implement `replay`

What was done:

- added a real `stagehand replay` subcommand to the Go CLI
- added replay-specific help text and flag parsing
- added support for loading a stored run by `--run-id`
- changed the command contract so `replay` requires a managed command after `--`
- changed session lookup so `--session` resolves the latest replayable `complete` run instead of the latest broken run
- added a minimal exact-replay source summarizer under `internal/runtime/replay`
- wrote the source run interactions to a replay-seed bundle and passed that file to the child process
- launched a managed child process in replay mode and captured its replay output bundle
- persisted the replayed interactions as a new replay-mode run through `internal/recording.Writer`
- returned machine-readable JSON output naming both source and replay run IDs, plus interaction counts, services, and fallback tiers
- added concrete replay errors for invalid selector usage, missing runs, and non-replay-eligible artifacts
- added store support and tests for latest-run-by-session lookup
- added CLI tests for replay by run ID, replay by session, missing-command rejection, and replay persistence behavior

Current outcome:

- the Go CLI now runs a real replay workflow instead of just printing a summary for a stored run
- users can seed a Stagehand-aware child process from a stored complete run and persist the replay result as a separate replay-mode run
- this is still exact replay, not simulator-backed fallback/runtime behavior, but it is now a real managed execution path

## 21. CLI `inspect`

Completed stories:

- Story F3: Implement `inspect`

What was done:

- added a real `stagehand inspect` subcommand to the Go CLI
- added selector support for `--run-id` and `--session`
- added store support for loading the latest run record for a session across all lifecycle states
- rendered ordered interactions directly from SQLite without requiring artifact hydration
- rendered service, operation, protocol, latency, and fallback tier per interaction
- rendered nested interaction trees from `parent_interaction_id`
- added `--show-bodies` to expand request bodies and event payloads on demand
- rendered integrity issues and uppercased lifecycle status so incomplete and corrupted runs are obvious
- added tests for selector validation, nested tree rendering, body expansion, and failed-run inspection by session

Current outcome:

- users can inspect successful and failed runs from the terminal without querying SQLite directly
- failed runs no longer require replay eligibility to be debugged
- the first usable local record/replay/inspect CLI loop now exists

## 22. TypeScript Bootstrap Runtime

Completed stories:

- Story G1: Implement TypeScript bootstrap

What was done:

- replaced the placeholder TypeScript `init()` with a real bootstrap/runtime module
- added `init({ session, mode, configPath? })`
- added `initFromEnv()` for CLI-managed subprocess workflows
- added singleton runtime guards plus explicit `AlreadyInitializedError`, `InvalidModeError`, and `NotInitializedError`
- added runtime metadata including session, mode, run ID, config path, SDK version, artifact version, and initialization timestamp
- aligned config autodiscovery behavior with Python by resolving `stagehand.yml` from the current working directory when present
- added TypeScript tests covering valid bootstrap, env-driven bootstrap, invalid mode rejection, empty-session rejection, missing config rejection, and runtime access before initialization

Current outcome:

- the TypeScript SDK is no longer scaffold-only at the bootstrap layer
- Node users now have the same basic init/runtime semantics as Python users
- built-in `fetch` and direct `undici.fetch(...)` traffic are now capturable through the shared dispatcher interception layer
- TypeScript now records normalized success, error, and timeout interactions in the shared artifact-shaped structure

## Current Implemented Surface

## Go

Implemented:

- version constants
- CLI and daemon scaffold binaries
- unit tests for binary output behavior
- config schema types
- config loading and validation
- config fixture validation tests
- recorder artifact schema types
- recorder artifact validation
- recorder fixture validation tests
- structural scrub pipeline
- structural scrub pipeline tests
- detector library
- detector library corpus tests
- session-salt manager
- session-salt manager tests
- recorder-side safe persisted writer
- end-to-end raw-SQLite secrecy tests
- CLI `record` managed subprocess command
- CLI `record` tests
- CLI `replay` managed subprocess command
- CLI `replay` tests
- CLI `inspect` command
- CLI `inspect` tests
- shared CLI workflow helpers for command execution, bundle import/export, and local master-key loading
- exact replay source summarizer
- custom scrub rule merge helpers
- custom scrub rule merge tests
- SQLite migration runner
- SQLite schema migration tests
- shared store interface
- SQLite store implementation
- SQLite store behavior tests
- reusable store contract test suite
- SQLite contract-test runner

Not yet implemented:

- runtime
- scrub engine
- simulators
- assertion engine
- diff engine

## Python

Implemented:

- package metadata
- version constants
- bootstrap `init()`
- bootstrap `init_from_env()`
- runtime metadata and singleton access helpers
- `httpx` sync and async interception
- normalized in-memory interaction capture
- capture-bundle export helpers
- OpenAI request detection
- OpenAI SSE capture with tool-call boundary events
- OpenAI exact replay from seeded captured interactions
- env-driven replay bundle loading for CLI-managed subprocess replay
- Python bootstrap tests
- Python interception tests

Not yet implemented:

- direct Python-to-SQLite persistence without the current CLI/import boundary
- direct Python loading of stored SQLite run artifacts without the current CLI-provided replay bundle

## TypeScript

Implemented:

- package metadata
- version constants
- bootstrap `init()`
- bootstrap `initFromEnv()`
- runtime metadata and singleton access helpers
- TypeScript bootstrap tests

Not yet implemented:

- request interception
- provider integrations
- replay integration beyond env-driven bootstrap wiring

## Docs and Planning

Implemented:

- build plan
- epic-to-milestone map
- epic breakdown
- config schema reference
- artifact schema reference
- schema compatibility reference
- structural scrub pipeline reference
- session hashing reference
- SQLite local store reference
- detector library reference
- product documentation set in `ProductDocumentations/`

## Current Quality Bar

The repo now has:

- local format/build/lint/test task runner
- GitHub Actions CI
- real Go tests
- real config validation tests
- real artifact validation tests
- real schema fixture validation tests
- real scrub pipeline tests
- real session-salt manager tests
- real SQLite migration tests
- real SQLite store tests
- real reusable storage contract tests
- Python smoke tests
- TypeScript smoke tests

This is still early-stage, but it is no longer just planning plus placeholders.

## Work Still Remaining on the Critical Path

The next major unfinished milestone items are:

1. Story P1: write threat model and security posture docs
2. replace the current CLI/SDK file-bundle handoff with a more direct recording boundary where appropriate
3. extend replay beyond exact seeded interactions into fuller runtime or simulator behavior
4. source the session-salt master key from a real runtime secret instead of test/local construction

## Current Risk Notes

- many internal directories are still layout placeholders only
- the current replay engine is still exact-match only and does not yet provide full simulator-backed fallback/runtime behavior
- Python `httpx` capture still carries placeholder scrub provenance while the SDK-side direct persistence path remains unfinished
- persisted Python traffic currently goes through the CLI/import bundle boundary rather than a direct SDK-to-store path
- session-salt encryption exists in Go local mode, but the final runtime master-key source is not wired yet
- inline custom scrub rules are supported, but external custom rule files are still deferred
- OpenAI replay inside the Python SDK still relies on seeded interactions; the CLI now supplies those seeds from persisted runs through a bundle file
- sample config files are defined before their downstream consumers exist
- Python package builds generate local artifacts, but those are not part of the tracked source
- the dashboard remains intentionally deferred

## Bottom Line

The repository currently has:

- real repo structure
- real CI
- real Go build/test enforcement
- real config schemas
- real sample config files
- real local SQLite store interface
- real Python `httpx` interception
- real Python OpenAI exact replay for the first demo
- real Go structural scrub pipeline
- real Go detector library for free-text secret and PII detection
- real Go session-scoped deterministic hashing with encrypted local salt persistence
- real recorder-side scrub-before-persist writes with raw-SQLite proof that standard secrets are not stored in plaintext
- real CLI `record` command that runs a managed subprocess and persists scrubbed captured interactions
- real CLI `replay` command that seeds a managed subprocess from a stored run and persists the replay result
- real Python env bootstrap and bundle export/import support for the CLI-managed workflow

It does not yet have the core product loop of:

- full simulator-backed replay beyond the current exact managed replay path

That loop remains the next meaningful implementation target.
