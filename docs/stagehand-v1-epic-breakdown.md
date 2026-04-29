# Stagehand V1 Epic Breakdown

## Purpose

This page expands the epic list into execution-ready stories and detailed to-do lists. It is the bridge between the high-level build plan and the actual work board.

Companion docs:

- `stagehand-v1-build-plan.md`
- `stagehand-v1-epic-milestone-map.md`
- `stagehand-v1-defect-remediation-plan.md`

## How to Use This Page

- Each epic has a goal, milestone span, dependencies, stories, and a detailed checklist.
- Stories are intentionally small enough to become tickets.
- Plan A is the default build.
- Plan B epics stay deferred unless explicitly pulled forward by design-partner demand.

## Story Status Convention

Use these statuses on your board:

- `not-started`
- `in-progress`
- `blocked`
- `done`

## Plan A Epics

## Epic A: Repo Scaffold, CI, Packaging Skeleton

- Epic code: `A`
- Milestone span: `M0`
- Estimate: `4d`
- Goal: establish the repo structure, basic automation, and packaging shells so work can proceed without setup churn.
- Depends on: none

### Story A1: Create monorepo skeleton

- Outcome: the repo layout exists for Go, Python, TypeScript, dashboard placeholder, docs, and examples.
- To do:
  - [x] create root folders matching the agreed repo layout
  - [x] initialize Go module for CLI and daemon code
  - [x] create Python package skeleton under `sdk/python`
  - [x] create TypeScript package skeleton under `sdk/typescript`
  - [x] create `dashboard/` placeholder with README noting deferred status in Plan A
  - [x] create `examples/` and `docs/` subfolders needed by the build plan
  - [x] add root `.gitignore`, `.editorconfig`, and formatting defaults

### Story A2: Add baseline CI automation

- Outcome: every PR can run formatting, linting, and test commands from a clean starting point.
- To do:
  - [x] add GitHub Actions workflow for Go, Python, and Node setup
  - [x] add placeholder jobs for lint and test in each language runtime
  - [x] add a root Makefile or task runner with consistent local commands
  - [x] add dependency caching where it does not complicate reliability
  - [x] make CI fail on formatting drift

### Story A3: Create packaging shells

- Outcome: the repo is ready to package the CLI and SDKs later without structural rework.
- To do:
  - [x] add `go build` target for CLI
  - [x] add Python packaging metadata
  - [x] add TypeScript package metadata and build script
  - [x] add version placeholders aligned with artifact versioning strategy
  - [x] document local build commands in the root README

### Epic A completion checklist

- [x] repo layout matches the planned structure
- [x] CI runs on PRs
- [x] local build commands exist for Go, Python, and TypeScript
- [x] packaging shells exist even if publishing is deferred

## Epic B: Config Schemas and Event Schemas

- Epic code: `B`
- Milestone span: `M0`
- Estimate: `5d`
- Goal: freeze the core data contracts before broad implementation begins.
- Depends on: `A`

### Story B1: Define config schemas

- Outcome: `stagehand.yml` and `stagehand.test.yml` have explicit fields, defaults, and validation rules.
- To do:
  - [x] define config sections for record, replay, scrub, fallback, auth, error injection, and CI behavior
  - [x] define config defaults and required fields
  - [x] document which config values can vary by environment
  - [x] write sample config files
  - [x] define validation errors for invalid config

### Story B2: Define run and event artifact schemas

- Outcome: recording artifacts have stable structure and versioning metadata.
- To do:
  - [x] define `Run` schema
  - [x] define `Interaction` schema
  - [x] define `Event` schema
  - [x] define `ScrubReport` schema
  - [x] add version fields: `schema_version`, `sdk_version`, `runtime_version`, `scrub_policy_version`
  - [x] define incomplete and corrupted run states

### Story B3: Define validation and compatibility rules

- Outcome: schema validation is testable and future compatibility has an explicit policy.
- To do:
  - [x] choose schema format or validation strategy
  - [x] add schema validation fixtures
  - [x] define minor-version compatibility rules
  - [x] define migration expectations for major versions
  - [x] document what changes are allowed without a schema version bump

### Epic B completion checklist

- [x] config schemas are written
- [x] artifact schemas are written
- [x] validation fixtures pass
- [x] compatibility policy is documented

## Epic C: SQLite Store and Migrations

- Epic code: `C`
- Milestone span: `M1`
- Estimate: `4d`
- Goal: provide a local-first storage backend that supports the first safe record/replay slice.
- Depends on: `B`

### Story C1: Define SQLite schema and migrations

- Outcome: the local database supports runs, interactions, events, assertions, baselines, and salts.
- To do:
  - [x] create initial schema migration
  - [x] add tables required by Plan A local mode
  - [x] add indexes for run lookup and interaction ordering
  - [x] add migration runner
  - [x] add rollback strategy or explicit non-goal note

### Story C2: Implement store interface

- Outcome: recorder and runtime can persist and query artifacts through a stable interface.
- To do:
  - [x] implement create/read/update operations for runs
  - [x] implement write/read operations for interactions and events
  - [x] implement scrub salt persistence
  - [x] implement baseline lookup and write paths
  - [x] implement atomic write boundary per interaction

### Story C3: Add storage contract tests

- Outcome: store behavior is validated before Postgres ever enters the picture.
- To do:
  - [x] write contract tests for run creation and completion state transitions
  - [x] write contract tests for interaction ordering
  - [x] write contract tests for corrupted/incomplete run handling
  - [x] write contract tests for deletion behavior

### Epic C completion checklist

- [x] migrations run from a clean repo
- [x] store interface is implemented
- [x] contract tests pass
- [x] local deletion behavior is defined

## Epic D: Python SDK First Slice

- Epic code: `D`
- Milestone span: `M1`
- Estimate: `8d`
- Goal: capture and replay a Python agent hitting OpenAI with minimal integration friction.
- Depends on: `A`, `B`, `C`

### Story D1: Implement Python bootstrap

- Outcome: users can initialize Stagehand with one line.
- To do:
  - [x] implement `stagehand.init(session, mode, config_path=None)`
  - [x] add runtime guard against double initialization
  - [x] add mode handling for `record`, `replay`, and `passthrough`
  - [x] expose run/session metadata to the recorder

### Story D2: Add HTTP interception

- Outcome: network requests can be captured without user code rewrites.
- To do:
  - [x] intercept `httpx`
  - [x] preserve request metadata, timing, and response metadata
  - [x] normalize captured data into the core interaction schema
  - [x] ensure errors and timeouts are captured as events, not dropped

### Story D3: Add OpenAI-aware capture and replay hooks

- Outcome: OpenAI exact replay works for the first demo.
- To do:
  - [x] detect OpenAI request shape
  - [x] capture SSE chunks in order
  - [x] preserve tool-call boundaries if present
  - [x] route replay responses back through the same calling surface
  - [x] add integration test against a sample Python agent

### Epic D completion checklist

- [x] Python init API works
- [x] `httpx` capture works
- [x] OpenAI exact replay works offline
- [x] integration test demonstrates the first demo flow

## Epic E: Scrubbing Engine Core

- Epic code: `E`
- Milestone span: `M1`
- Estimate: `10d`
- Goal: make persisted recordings safe enough to store and share while preserving replay fidelity.
- Depends on: `B`

### Story E1: Build structural scrub pipeline

- Outcome: JSON bodies, headers, and query parameters can be scrubbed through rules.
- To do:
  - [x] implement payload traversal
  - [x] implement rule matching by path
  - [x] support `drop`, `mask`, `hash`, and `preserve`
  - [x] add request header handling for auth and cookies
  - [x] generate scrub reports from the applied rules

### Story E2: Build detector library

- Outcome: free-text and shape-independent secrets can be caught.
- To do:
  - [x] add email detector
  - [x] add JWT detector
  - [x] add phone detector
  - [x] add SSN detector
  - [x] add Luhn-validated card detector
  - [x] add common API key prefix detector
  - [x] add test corpus with positive and negative cases

### Story E3: Add session-scoped deterministic hashing

- Outcome: the same sensitive value is replaced consistently inside one session.
- To do:
  - [x] generate per-session salts
  - [x] encrypt salts at rest
  - [x] implement deterministic hash replacement helpers
  - [x] ensure different sessions do not share replacements
  - [x] test replay behavior against scrubbed identifiers

### Story E4: Add user-configurable scrub rules

- Outcome: teams can extend scrubbing with org-specific rules.
- To do:
  - [x] define custom scrub rule format in config
  - [x] merge custom rules with defaults
  - [x] validate conflicting rules
  - [x] document custom rule examples

### Epic E completion checklist

- [x] standard secrets are not persisted in plaintext
- [x] session hashing is deterministic within a session
- [x] scrub reports are generated
- [x] custom scrub rules are supported

## Epic F: CLI First Slice

- Epic code: `F`
- Milestone span: `M1`
- Estimate: `4d`
- Goal: provide a usable local workflow for record, replay, and inspect.
- Depends on: `C`, `D`, `E`

### Story F1: Implement `record`

- Outcome: the CLI can start and manage a recording run.
- To do:
  - [x] define command flags and help text
  - [x] support session name and config file inputs
  - [x] create run records in storage
  - [x] mark run completion or failure cleanly
- Current implementation note:
  `record` now launches a managed subprocess, imports its exported interaction bundle, persists interactions through the scrub-before-persist writer, and finalizes the run in SQLite.

### Story F2: Implement `replay`

- Outcome: a stored run can be replayed locally against the simulator/runtime path.
- To do:
  - [x] load run by ID or session
  - [x] route replay through exact-match logic
  - [x] surface replay errors with concrete messages
  - [x] emit machine-readable result output
- Current implementation note:
  `replay` now loads a stored complete run, seeds a managed subprocess through a replay bundle, persists the replayed interactions as a new replay-mode run, and emits machine-readable JSON output.

### Story F3: Implement `inspect`

- Outcome: users can debug a run without querying SQLite directly.
- To do:
  - [x] render ordered interactions
  - [x] show service, operation, latency, fallback tier
  - [x] show nested interaction tree
  - [x] add basic body expansion control
  - [x] mark incomplete or corrupted runs clearly
- Current implementation note:
  `inspect` renders the latest run by session or an explicit run ID from SQLite, shows lifecycle status and integrity issues, and supports `--show-bodies` for expanded request and event payload output.

### Epic F completion checklist

- [x] `record` works
- [x] `replay` works
- [x] `inspect` is usable on failed runs
- [x] command help and output formats are documented

## Epic G: TypeScript SDK First Slice

- Epic code: `G`
- Milestone span: `M2`
- Estimate: `11d`
- Goal: offer parity for Node-based agent teams without proxy setup.
- Depends on: `B`, `C`, `E`, `F`

### Story G1: Implement TypeScript bootstrap

- Outcome: Node users get an equivalent init surface.
- To do:
  - [x] define SDK init API mirroring Python behavior
  - [x] implement mode handling
  - [x] propagate session and run metadata
  - [x] align config loading behavior with Python
- Current implementation note:
  the TypeScript SDK now has `init`, `initFromEnv`, singleton runtime guards, run/session metadata, and `stagehand.yml` autodiscovery parity with the Python bootstrap.

### Story G2: Add request interception

- Outcome: common Node HTTP paths are capturable.
- To do:
  - [x] intercept `fetch`
  - [x] intercept `undici`
  - [x] normalize requests and responses into shared artifact shape
  - [x] capture error and timeout paths
- Current implementation note:
  the TypeScript SDK now installs request interception through the shared `undici` global dispatcher in non-`passthrough` modes, captures built-in `fetch` and direct `undici.fetch(...)` traffic, normalizes those requests into the shared artifact-shaped interaction model, records network failures and aborted requests as terminal `error` or `timeout` events, emits OpenAI-style SSE `stream_chunk` / `stream_end` / `tool_call_start` / `tool_call_end` events during capture, exact-replays seeded non-streaming interactions in `replay` mode, and exact-replays supported SSE interactions without live network dispatch. Replay misses still fail closed before live network dispatch.

### Story G3: Add parity tests

- Outcome: Python and TS produce equivalent artifacts for equivalent supported flows, and any remaining gaps are explicit.
- To do:
  - [x] create shared fixture scenarios for overlapping Python and TS capture paths
  - [x] compare canonical interaction fields and event ordering
  - [x] verify scrub integration produces equivalent stored artifacts through the shared persisted writer path
  - [x] document known parity gaps that are outside the supported overlap
- Required shared scenarios:
  - [x] simple HTTP `GET` success fixture
  - [x] simple HTTP `GET` timeout fixture
  - [x] OpenAI non-streaming chat success fixture on default host
  - [x] OpenAI non-streaming chat success fixture on configured custom host
  - [x] OpenAI SSE success fixture with `stream_chunk`, `stream_end`, and tool-call boundary events
  - [x] scrub parity fixture that proves the same request/response header rules and `redacted_paths` behavior after persistence
- Canonical parity checks:
  - [x] `service`, `operation`, `protocol`, `streaming`, and `fallback_tier`
  - [x] request method, path, selected headers, and normalized body shape
  - [x] event `sequence`, `type`, and selected event payload fields
  - [x] `scrub_report.scrub_policy_version` and `scrub_report.session_salt_id`
  - [x] persisted scrubbed artifact shape after the Go writer path
- Current implementation note:
  the shared parity suite now covers simple HTTP success/timeout, OpenAI non-streaming success on default and configured custom hosts, OpenAI SSE capture with stream and tool-call events, and a shared scrub parity fixture backed by the Go persisted writer path. Request-body parity for body-bearing SDK flows is intentionally normalized to `{"capture_kind":"present"}` because the current Node `fetch`/`undici` dispatch surface still exposes opaque request-body objects in some paths.

### Story G4: Add TypeScript streaming replay parity

- Outcome: supported TypeScript SSE captures can be exact-replayed without live network dispatch.
- To do:
  - [x] reconstruct SSE bytes from captured `stream_chunk` and `stream_end` events
  - [x] emit replayed chunks through the Undici handler callbacks in original order
  - [x] preserve captured tool-call boundaries during replay
  - [x] fail closed with typed errors for unsupported streaming shapes
  - [x] add replay parity tests showing zero live server hits during streaming replay
- Required scenarios:
  - [x] seeded chat-completions SSE replay returns `text/event-stream` and zero live hits
  - [x] seeded responses SSE replay preserves ordered chunk delivery and final done marker
  - [x] seeded streaming timeout or error returns typed replay failure
  - [x] streaming replay miss fails closed before outbound dispatch
- Current implementation note:
  TypeScript exact replay now supports supported SSE captures by reconstructing captured stream bytes from `stream_chunk` and `stream_end`, delivering them through the Undici handler callbacks in original order, preserving the captured tool-call boundary events in the replayed artifact, and failing closed with typed replay errors when a seeded streaming interaction is malformed or unsupported.

### Epic G completion checklist

- [x] TS SDK init works
- [x] `fetch` and `undici` are covered
- [x] shared parity fixtures exist for overlapping Python and TS flows
- [x] scrub parity is verified through the shared persisted writer path
- [x] TypeScript streaming replay is exact for supported SSE flows or explicitly fail-closed for unsupported shapes
- [x] major config behavior matches Python

## Epic H: Runtime Core

- Epic code: `H`
- Milestone span: `M2-M3`
- Estimate: `12d`
- Goal: support sessions, snapshots, queued events, and fallback logic for realistic replay.
- Depends on: `B`, `C`

### Story H1: Implement session lifecycle

- Outcome: sessions can be created, forked, restored, and destroyed cleanly.
- To do:
  - [x] define session state model
  - [x] implement create
  - [x] implement fork
  - [x] implement snapshot
  - [x] implement restore
  - [x] implement destroy
  - [x] add session isolation tests
- Current implementation note:
  runtime sessions now have durable `sessions` and `session_snapshots` rows, a Go `internal/runtime/session.Manager`, forked child snapshots link back to parent snapshots, restore rejects cross-session snapshots, and destroy removes runtime session state along with the existing session-scoped local artifacts.

### Story H2: Implement event queue and sim-time handling

- Outcome: delayed simulator events can be scheduled and replayed predictably.
- To do:
  - [x] define scheduled event schema
  - [x] implement queue persistence
  - [x] implement non-virtualized sim clock semantics
  - [x] implement `advance_time`
  - [x] add tests for push and pull delivery modes
- Current implementation note:
  runtime event queues now have durable `session_clocks` and `scheduled_events` rows, a Go `internal/runtime/queue.Manager`, explicit non-virtualized `AdvanceTime` semantics, deterministic due-event ordering, push delivery during time advancement, pull delivery on demand, and store-level cleanup through session deletion.

### Story H3: Implement fallback tiers 0-2

- Outcome: replay can recover from non-exact matches with explicit tier tracking.
- To do:
  - [x] implement exact request matching
  - [x] implement nearest-neighbor matching
  - [x] implement state-aware synthesis hook
  - [x] persist used fallback tier on each interaction
  - [x] add configuration to allow and disallow tiers
- Current implementation note:
  the Go runtime now has `internal/runtime/fallback.Matcher` for tier 0 exact matching, tier 1 nearest-neighbor matching, and tier 2 state-synthesis hooks. Matching honors `fallback.allowed_tiers`, returns interactions with explicit `fallback_tier`, and SQLite persistence now has regression coverage for round-tripping the used tier. Tier 3 `llm_synthesis` remains intentionally rejected by the H3 matcher.

### Story H4: Implement runtime resilience behavior

- Outcome: partial failures are observable and do not silently corrupt replay.
- To do:
  - [x] mark runs `complete`, `incomplete`, or `corrupted`
  - [x] ensure interaction writes are atomic
  - [x] validate imports before acceptance
  - [x] surface runtime failures with actionable errors
- Current implementation note:
  CLI-managed runs now classify terminal failures as `complete`, `incomplete`, or `corrupted` with concrete integrity issue codes. Imported interaction bundles are normalized and structurally validated before persistence starts, invalid imports write zero interactions, and SQLite keeps the per-interaction transactional write boundary.

### Epic H completion checklist

- [x] sessions are isolated
- [x] snapshots restore correctly
- [x] event queue works
- [x] fallback tiers are visible and configurable
- [x] runtime failure modes are handled cleanly

## Epic I: Stripe Simulator and Error Injection

- Epic code: `I`
- Milestone span: `M3`
- Estimate: `12d`
- Goal: prove the product wedge with a stateful destructive API and realistic failure testing.
- Depends on: `H`, `E`

### Story I1: Implement Stripe subset

- Outcome: the Stripe simulator supports a narrow but compelling object set.
- To do:
  - [x] finalize V1 subset: customer, payment method, and payment intent or charge
  - [x] define request matching and response schemas for the subset
  - [x] store object state by session
  - [x] implement create, read, and update flows required by examples
- Current implementation note:
  the Stripe simulator core now supports `customer`, `payment_method`, and `payment_intent` objects, declares route matches for the supported `/v1` paths, persists object state under runtime session snapshots, and implements create/read/update plus payment-method attach flows. Webhooks, realistic state-transition rules, and error injection remain in I2/I3.

### Story I2: Add Stripe business rules and side effects

- Outcome: replay is more than a static mock.
- To do:
  - [x] enforce state consistency rules
  - [x] reject invalid state transitions with realistic errors
  - [x] schedule webhooks for supported flows
  - [x] extract entities for customer identity
- Current implementation note:
  the Stripe simulator now enforces payment method/customer consistency, validates supported PaymentIntent transitions, returns Stripe-shaped invalid-request and resource-missing errors, schedules persisted webhook events for supported customer, payment method, and payment intent flows, and exposes customer identity extraction from customer and attached payment method data.

### Story I3: Implement error injection

- Outcome: users can force realistic failures and prove agent behavior in CI.
- To do:
  - [x] implement matcher by service, operation, nth call, and probability
  - [x] implement response override injection
  - [x] add named error library entries
  - [x] persist injection provenance in run metadata
  - [x] add tests for deterministic third-call failure behavior
- Current implementation note:
  the runtime injection engine now evaluates ordered rules by service, operation, call count, and optional probability; returns explicit response overrides; includes named Stripe error entries; records applied-rule provenance under run metadata; and SQLite persists top-level run metadata through `runs.metadata_json`. The Stripe simulator calls the engine before supported operations, returns injected Stripe-shaped errors, records provenance, and does not mutate state for injected failures. The executable failure-injection demo lives in `examples/failure-injection-demo` and is covered by `TestFailureInjectionDemoPasses`.

### Epic I completion checklist

- [x] Stripe subset is usable for the onboarding/refund examples
- [x] webhook scheduling works for supported flows
- [x] named error injection works
- [x] the failure-injection demo passes

## Epic J: Assertion Engine

- Epic code: `J`
- Milestone span: `M4`
- Estimate: `6d`
- Goal: let users express expected behavior as machine-enforced checks.
- Depends on: `H`, `I`, `K`

### Story J1: Implement assertion schema and parser

- Outcome: assertion files validate before execution.
- To do:
  - [x] define assertion YAML schema
  - [x] implement parser
  - [x] validate field combinations per assertion type
  - [x] return clear validation errors
- Current implementation note:
  assertion files now parse through `internal/analysis/assertions`, reject unknown YAML fields, validate the six planned assertion types (`count`, `ordering`, `payload-field`, `forbidden-operation`, `fallback-prohibition`, and `cross-service`), and return indexed field-path validation errors before any execution step.

### Story J2: Implement core assertion types

- Outcome: the common behavioral checks are executable.
- To do:
  - [x] implement `count`
  - [x] implement `ordering`
  - [x] implement `payload-field`
  - [x] implement `forbidden-operation`
  - [x] implement `fallback-prohibition`
- Current implementation note:
  the assertion evaluator now executes the five core assertion types against `recorder.Run` interactions and returns per-assertion `passed`, `failed`, or `unsupported` results with concrete evidence, including matched interaction IDs, violating interaction IDs, observed counts, actual payload values, and disallowed fallback tiers. `cross-service` remains parsed but explicitly `unsupported` until J3.

### Story J3: Implement cross-service assertions

- Outcome: assertions can span entities across service boundaries.
- To do:
  - [x] define minimal rule-evaluation model
  - [x] add entity lookup helpers
  - [x] support 1-hop and 2-hop matching
  - [x] produce evidence-rich failure output
- Current implementation note:
  `cross-service` assertions now execute through shallow entity extraction on the declared left and right references, support `equals` links across direct scalar paths and nested scalar leaves, and return evidence for extracted left entities, right entities, and linked pairs.

### Epic J completion checklist

- [x] all six assertion types are implemented
- [x] schema validation catches invalid files
- [x] failure output names concrete interactions for core assertion types
- [x] cross-service rules work for at least one example flow

## Epic K: Diff Engine and Baseline Logic

- Epic code: `K`
- Milestone span: `M3-M4`
- Estimate: `6d`
- Goal: compare runs meaningfully and support baseline-driven regression detection.
- Depends on: `B`, `C`, `H`

### Story K1: Implement baseline storage and selection rules

- Outcome: runs can be promoted to baselines and selected predictably in CI.
- To do:
  - [x] define baseline schema and write path
  - [x] implement baseline creation command or API
  - [x] define tie-breaking and branch-selection rules
  - [x] document when incomplete runs are ineligible
- Current implementation note:
  SQLite baselines are stored as `baseline_id`, `session_name`, `source_run_id`, `git_sha`, and `created_at`. `stagehand baseline promote --run-id <id>` promotes a complete stored run to a session baseline, generates a baseline ID when one is not supplied, and emits machine-readable JSON. Local latest-baseline selection is deterministic by `created_at DESC, baseline_id DESC`; branch selection is currently a test-runner config concern through `stagehand.test.yml baseline.branch`, while local baseline rows remain scoped by session. Running, incomplete, and corrupted runs are ineligible because `PutBaseline` requires the source run lifecycle status to be `complete`.

### Story K2: Implement diff alignment

- Outcome: interactions can be aligned in a stable way before user-facing reporting.
- To do:
  - [x] align runs by session and sequence
  - [x] detect added and removed interactions
  - [x] detect ordering changes
  - [x] detect fallback-tier regressions
  - [x] add field-ignore support
- Current implementation note:
  `internal/analysis/diff` now compares two same-session runs, aligns interactions by a deterministic request lane plus occurrence count, and orders output by baseline sequence with candidate-only additions placed by candidate sequence. It reports structured `added`, `removed`, `modified`, `ordering_changed`, and `fallback_regression` changes. Run-local and timing fields such as `run_id`, `interaction_id`, `sequence`, `fallback_tier`, `latency_ms`, `events[*].t_ms`, and `events[*].sim_t_ms` are ignored for generic modification checks so ordering, fallback regressions, and semantic payload changes are reported through their dedicated paths. Callers can pass additional ignore paths such as `request.body.request_id`, `events[*].data.request_id`, or `events.0.data.trace_id` to suppress dynamic fields. Alignment is per-endpoint occurrence; reordering same-endpoint requests with different bodies is reported as modifications, not as ordering changes.

### Story K3: Implement diff renderers

- Outcome: diffs are useful in terminal, JSON, and CI comment outputs.
- To do:
  - [x] emit machine-readable JSON diff
  - [x] emit terminal summary
  - [x] emit GitHub markdown summary
  - [x] separate failing from informational diffs
- Current implementation note:
  `internal/analysis/diff` now exposes renderers for indented machine-readable JSON, terminal summaries, and GitHub markdown comments. JSON output includes full changes plus separate `failing_changes` and `informational_changes` arrays. Terminal and markdown output include the same summary counts and split sections. Added, removed, modified, and fallback-regression changes are currently classified as failing; ordering changes are informational until CI policy promotes them.

### Epic K completion checklist

- [x] baseline selection is deterministic
- [x] run alignment works on the examples
- [x] diff renderers exist for terminal, JSON, and GitHub markdown
- [x] fallback regressions are clearly surfaced

## Epic L: GitHub Action

- Epic code: `L`
- Milestone span: `M4`
- Estimate: `3d`
- Goal: make replay and assertions a normal PR workflow.
- Depends on: `F`, `J`, `K`

### Story L1: Wrap the CLI for GitHub Actions

- Outcome: the action can run replay and diff with a small config surface.
- To do:
  - [ ] define action inputs
  - [ ] wire inputs to CLI commands
  - [ ] support session list and baseline source
  - [ ] support fail conditions for assertions and fallback regressions

### Story L2: Add artifact and PR reporting

- Outcome: CI leaves usable output behind.
- To do:
  - [ ] upload run artifacts
  - [ ] upload diff reports
  - [ ] post PR markdown summary
  - [ ] link to local inspection guidance if hosted is not used

### Epic L completion checklist

- [ ] action runs in a sample repository
- [ ] PR comments are readable
- [ ] artifacts upload successfully

## Epic M: Conformance Harness First Pass

- Epic code: `M`
- Milestone span: `M5-M6`
- Estimate: `7d`
- Goal: detect simulator drift early enough that fidelity does not quietly decay.
- Depends on: `I`, `N`

### Story M1: Define conformance case model

- Outcome: simulator-owned conformance cases have a reusable format.
- To do:
  - [ ] define case inputs
  - [ ] define real-service credential requirements
  - [ ] define tolerated diff fields
  - [ ] define match/fail result shape

### Story M2: Implement real-vs-sim runner

- Outcome: a case can run once against the real API and once against the simulator.
- To do:
  - [ ] implement real-service runner for OpenAI
  - [ ] implement real-service runner for Stripe test mode
  - [ ] implement simulator runner path
  - [ ] capture structural diffs

### Story M3: Add nightly execution and result storage

- Outcome: drift is visible and trackable.
- To do:
  - [ ] create nightly workflow
  - [ ] store conformance results
  - [ ] highlight new drift
  - [ ] document cost and run-frequency expectations

### Epic M completion checklist

- [ ] OpenAI smoke conformance runs
- [ ] Stripe smoke conformance runs
- [ ] nightly runner exists
- [ ] drift results are stored and reviewable

## Epic N: Generic HTTP Simulator

- Epic code: `N`
- Milestone span: `M2`
- Estimate: `4d`
- Goal: cover long-tail APIs without exploding simulator count.
- Depends on: `H`

### Story N1: Implement exact replay path

- Outcome: generic HTTP can replay recorded responses with no service-specific logic.
- To do:
  - [ ] implement generic request matching
  - [ ] store and replay status, headers, and body
  - [ ] normalize dynamic but ignorable fields where necessary

### Story N2: Implement nearest-neighbor path

- Outcome: simple request variation does not always force a miss.
- To do:
  - [ ] define mutable field heuristics
  - [ ] support pagination and timestamp-like values
  - [ ] record why a tier-1 match was selected
  - [ ] expose tier selection in artifacts

### Story N3: Document limitations

- Outcome: users know when generic HTTP is enough and when it is not.
- To do:
  - [ ] document supported behavior
  - [ ] document unsupported stateful semantics
  - [ ] show when to request a dedicated simulator

### Epic N completion checklist

- [ ] exact replay works
- [ ] tier-1 matching works for simple variance
- [ ] limitations are documented

## Epic O: Example Flows, Docs, Packaging

- Epic code: `O`
- Milestone span: `M5-M6`
- Estimate: `8d`
- Goal: make the product understandable, runnable, and launchable.
- Depends on: `D`, `I`, `J`, `K`, `L`

### Story O1: Build the example flows

- Outcome: first-party examples double as demos and regression tests.
- To do:
  - [ ] create onboarding example
  - [ ] create refund example
  - [ ] create support escalation example
  - [ ] wire examples into automated test paths where possible
  - [ ] ensure each example demonstrates one core product capability
- Verification-agent slice:
  - [x] add simplest Python OpenAI record/replay/inspect verification agent
  - [x] add Python multi-step tool-calling verification agent
  - [x] add Python sensitive-data scrub verification agent
  - [x] add verifier that runs agents in order and checks SQLite persistence, scrub reports, replay output, and inspect output
  - [ ] run the verifier against a real `OPENAI_API_KEY` in an environment allowed to call OpenAI

### Story O2: Write docs

- Outcome: external users can self-serve the happy path.
- To do:
  - [ ] write getting-started guide
  - [ ] write scrubbing guide
  - [ ] write error injection guide
  - [ ] write limitations page
  - [ ] write baseline and CI usage guide

### Story O3: Finalize packaging and installability

- Outcome: the CLI and SDKs are installable by external users.
- To do:
  - [ ] finalize build and release commands
  - [ ] verify installation paths for CLI, Python, and TypeScript
  - [ ] write version and release notes template
  - [ ] create sample install commands for docs

### Epic O completion checklist

- [ ] three example flows exist
- [ ] core docs exist
- [ ] install steps are documented and tested
- [ ] examples serve as regression fixtures

## Epic P: Security Hardening for OSS Launch

- Epic code: `P`
- Milestone span: `M0, M1, M5-M6`
- Estimate: `6d`
- Goal: make the product defensible for a tool that intercepts API traffic and stores sensitive data.
- Depends on: `B`, `E`, `C`

### Story P1: Write threat model and security posture docs

- Outcome: the project has a declared security posture before launch.
- To do:
  - [ ] write initial threat model
  - [ ] document trust boundaries
  - [ ] document secret handling
  - [ ] document local and hosted risk assumptions

### Story P2: Implement retention and deletion behavior

- Outcome: stored recordings can be removed intentionally and predictably.
- To do:
  - [ ] define retention defaults
  - [ ] implement `delete-run`
  - [ ] implement `delete-session`
  - [ ] ensure salts are deleted with associated sessions
  - [ ] write tests for deletion behavior

### Story P3: Add launch-time security controls

- Outcome: OSS launch does not ship with obvious avoidable gaps.
- To do:
  - [ ] enable dependency scanning in CI
  - [ ] enable secret scanning in CI
  - [ ] publish vulnerability disclosure policy
  - [ ] verify no plaintext auth artifacts persist in standard paths

### Epic P completion checklist

- [ ] threat model exists
- [ ] deletion exists
- [ ] retention defaults are documented
- [ ] disclosure policy is published
- [ ] CI security scans run

## Epic Q: Design Partner Recruiting and Feedback Ops

- Epic code: `Q`
- Milestone span: `M2-M6`
- Estimate: `8d`
- Goal: ensure roadmap decisions are informed by real users before hosted and advanced integrations are built.
- Depends on: `D`, `F`

### Story Q1: Recruit design partners

- Outcome: a small set of real teams is ready to test the wedge.
- To do:
  - [ ] define partner profile
  - [ ] build outreach list
  - [ ] write concise outreach message
  - [ ] track outreach, replies, and fit
  - [ ] secure 3-5 active design partners

### Story Q2: Run structured onboarding and feedback sessions

- Outcome: feedback becomes comparable rather than anecdotal.
- To do:
  - [ ] define onboarding script
  - [ ] define feedback template
  - [ ] record blocked workflows
  - [ ] classify feedback as launch blocker, partner-specific, or later
  - [ ] review findings at the end of each milestone

### Story Q3: Feed roadmap decisions back into planning

- Outcome: partner input changes the roadmap in an explicit way.
- To do:
  - [ ] reserve 20 percent milestone capacity for partner blockers
  - [ ] track requested integrations
  - [ ] decide whether any deferred Plan B epic should move forward
  - [ ] document why requests are accepted or deferred

### Epic Q completion checklist

- [ ] outreach list exists
- [ ] at least 3 design partners have seen a demo
- [ ] feedback is categorized and tracked
- [ ] roadmap adjustments are explicit

## Epic R: Distribution Work

- Epic code: `R`
- Milestone span: `M5-M6`
- Estimate: `7d`
- Goal: make the OSS launch discoverable and coherent.
- Depends on: `O`, `Q`

### Story R1: Build launch assets

- Outcome: there is enough material for a credible public launch.
- To do:
  - [ ] write landing page copy
  - [ ] record 3-minute demo video
  - [ ] gather screenshots and terminal captures
  - [ ] prepare architecture and workflow diagrams if needed

### Story R2: Prepare launch messaging

- Outcome: the launch tells a clear wedge story.
- To do:
  - [ ] draft Show HN post
  - [ ] draft blog post or launch article
  - [ ] draft short-form social posts
  - [ ] tailor messaging for AI platform engineers

### Story R3: Run outreach and launch follow-up

- Outcome: launch traffic converts into real users and conversations.
- To do:
  - [ ] compile outreach list of engineers and founders
  - [ ] send launch notes to design partners and warm contacts
  - [ ] monitor launch responses
  - [ ] route feedback into Epic Q workflow

### Epic R completion checklist

- [ ] demo video exists
- [ ] launch post exists
- [ ] landing page copy exists
- [ ] outreach list exists

## Plan B Deferred Epics

These epics are not part of the recommended 6-month V1. Do not start them unless the roadmap is explicitly changed.

## Epic S: Gmail Dedicated Simulator

- Epic code: `S`
- Milestone span: `M7`
- Estimate: `6d`
- Goal: provide a dedicated Gmail simulation path only if design partners prove generic HTTP is insufficient.
- Depends on: `H`, `N`, `Q`

### Story S1: Define Gmail subset

- Outcome: a narrow supported API surface is documented.
- To do:
  - [ ] choose supported Gmail operations
  - [ ] define stored message model
  - [ ] define send and lookup behavior

### Story S2: Implement message state and replay behavior

- Outcome: email sends and reads have session-scoped state.
- To do:
  - [ ] implement send path
  - [ ] implement message persistence
  - [ ] implement query path for supported operations
  - [ ] add entity extraction for recipient identities

### Story S3: Add conformance smoke coverage

- Outcome: Gmail simulator does not drift silently.
- To do:
  - [ ] add smoke test against real Gmail or chosen sandbox path
  - [ ] compare simulator output structurally
  - [ ] document unsupported behavior

### Epic S completion checklist

- [ ] supported Gmail subset is documented
- [ ] message state works
- [ ] smoke conformance exists

## Epic T: Slack Dedicated Simulator

- Epic code: `T`
- Milestone span: `M7`
- Estimate: `6d`
- Goal: provide a dedicated Slack Web API simulation path only if partner demand requires it.
- Depends on: `H`, `N`, `Q`

### Story T1: Define Slack subset

- Outcome: the supported Slack operations are explicit.
- To do:
  - [ ] choose channel lookup and message operations
  - [ ] define thread behavior scope
  - [ ] document unsupported RTM/WebSocket behavior if deferred

### Story T2: Implement session-scoped Slack state

- Outcome: channel and message operations persist in-session.
- To do:
  - [ ] implement channel lookup
  - [ ] implement message send
  - [ ] implement basic thread support if needed
  - [ ] add entity extraction for users and channels

### Story T3: Add conformance smoke coverage

- Outcome: Slack behavior is checked against reality at a minimal level.
- To do:
  - [ ] create smoke conformance cases
  - [ ] compare status and body structure
  - [ ] document known gaps

### Epic T completion checklist

- [ ] supported Slack subset is documented
- [ ] channel and message state works
- [ ] smoke conformance exists

## Epic U: Postgres Wire Protocol

- Epic code: `U`
- Milestone span: `M8`
- Estimate: `18d`
- Goal: add database-level coverage only after the local core loop is proven externally.
- Depends on: `H`, `Q`

### Story U1: Scope V1.1/V2 Postgres coverage

- Outcome: the project does not accidentally commit to arbitrary SQL emulation.
- To do:
  - [ ] define supported Postgres operations
  - [ ] define unsupported query and transaction behavior
  - [ ] choose capture and replay boundary
  - [ ] define expected design-partner use cases

### Story U2: Implement protocol capture and replay

- Outcome: supported Postgres traffic can be recorded and replayed.
- To do:
  - [ ] implement protocol parsing for supported flows
  - [ ] capture queries, results, and errors
  - [ ] replay result sets deterministically
  - [ ] ensure session isolation for DB state where supported

### Story U3: Add reliability and conformance coverage

- Outcome: Postgres support is stable enough for real partner testing.
- To do:
  - [ ] add failure-path tests
  - [ ] add contract tests for supported flows
  - [ ] add design-partner scenario tests
  - [ ] document unsupported SQL semantics clearly

### Epic U completion checklist

- [ ] supported DB scope is narrow and explicit
- [ ] supported queries replay correctly
- [ ] failure paths are tested
- [ ] at least one partner scenario works

## Epic V: Hosted API, Auth, Tenancy

- Epic code: `V`
- Milestone span: `M9`
- Estimate: `12d`
- Goal: provide the minimum hosted backend needed for shared runs and baselines.
- Depends on: `K`, `P`, `Q`

### Story V1: Define hosted data and auth model

- Outcome: tenant boundaries are explicit before implementation.
- To do:
  - [ ] define org and team model
  - [ ] define user access model
  - [ ] define hosted run and baseline APIs
  - [ ] define minimal auth flow

### Story V2: Implement hosted run and baseline APIs

- Outcome: external teams can view and share run metadata centrally.
- To do:
  - [ ] implement run list API
  - [ ] implement run detail API
  - [ ] implement baseline list and selection API
  - [ ] add org-scoped query filters
  - [ ] add audit logging where feasible

### Story V3: Add tenancy and security checks

- Outcome: one tenant cannot view another tenant's data.
- To do:
  - [ ] add tenancy middleware
  - [ ] add authorization checks
  - [ ] test cross-tenant isolation
  - [ ] document hosted retention defaults

### Epic V completion checklist

- [ ] hosted API surface is minimal and explicit
- [ ] auth works
- [ ] tenant isolation is tested
- [ ] baselines are shareable across a team

## Epic W: Dashboard

- Epic code: `W`
- Milestone span: `M9`
- Estimate: `10d`
- Goal: provide a basic hosted UI for runs, timelines, baselines, and diffs.
- Depends on: `V`

### Story W1: Implement run list and run detail views

- Outcome: teams can browse runs without the CLI.
- To do:
  - [ ] build run list page
  - [ ] build run detail page
  - [ ] show interaction timeline
  - [ ] show status and fallback tiers

### Story W2: Implement baseline and diff views

- Outcome: baseline comparison is visible in the hosted UI.
- To do:
  - [ ] build baseline selection UI
  - [ ] build diff view
  - [ ] show failing vs informational changes clearly
  - [ ] support linkable run/diff pages

### Story W3: Implement assertion and conformance views

- Outcome: core trust signals are visible in one place.
- To do:
  - [ ] show assertion results
  - [ ] show conformance drift summary
  - [ ] show links back to artifacts or CLI-friendly references
  - [ ] document remaining UI gaps

### Epic W completion checklist

- [ ] run list exists
- [ ] run detail timeline exists
- [ ] diff view exists
- [ ] assertion and conformance summaries exist

## Epic X: Hosted Ops and Integration Bugs

- Epic code: `X`
- Milestone span: `M9-M10`
- Estimate: `10d`
- Goal: stabilize the hosted beta enough that it reduces support burden instead of increasing it.
- Depends on: `V`, `W`

### Story X1: Fix hosted integration gaps

- Outcome: design partners can use hosted without constant manual intervention.
- To do:
  - [ ] collect hosted onboarding failures
  - [ ] fix auth and session issues
  - [ ] fix API and UI mismatches
  - [ ] improve hosted error reporting

### Story X2: Add hosted operational guardrails

- Outcome: the hosted beta is supportable.
- To do:
  - [ ] add health checks
  - [ ] add structured logs
  - [ ] add basic monitoring and alerting
  - [ ] define backup and restore expectations

### Story X3: Close the beta hardening loop

- Outcome: the beta has a clear stability bar.
- To do:
  - [ ] review top hosted bugs weekly
  - [ ] re-test critical partner workflows
  - [ ] document known issues
  - [ ] decide whether hosted is ready for broader rollout

### Epic X completion checklist

- [ ] top hosted onboarding failures are fixed
- [ ] health and monitoring exist
- [ ] known issues are documented
- [ ] hosted beta has an explicit go/no-go review

## Suggested Board Breakdown

If you want to turn this page into tickets immediately, use this pattern:

1. Create one epic issue per epic.
2. Create one story issue per story.
3. Copy the story to-do list into the story issue body.
4. Link each story to its milestone and dependencies.
5. Do not create Plan B stories until the roadmap changes.

## Immediate Next Stories

If execution starts now, open these first:

1. `A1` Create monorepo skeleton
2. `A2` Add baseline CI automation
3. `B1` Define config schemas
4. `B2` Define run and event artifact schemas
5. `P1` Write threat model and security posture docs

Do not persist `D2` captures as durable artifacts before `E1` exists in at least a minimal form. In-memory interception can land earlier, but stored recordings must not bypass scrubbing.
