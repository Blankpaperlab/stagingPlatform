# Stagehand V1 Defect Remediation And Test Plan

## Purpose

This document turns the current repo audit into execution work. It focuses only on defects that are still live in the current tree, plus the test additions needed to keep them from regressing.

It is not a general roadmap. It is a targeted hardening plan for:

- correctness
- replay safety
- deterministic scrubbing
- cross-SDK parity
- test-suite strength

## Scope

Included here:

- confirmed live defects in the current codebase
- the fix order
- concrete code areas to change
- acceptance criteria
- concrete test cases to add
- regression-gate commands

Excluded here:

- findings already fixed in the current tree
- broad V2 architecture work
- hosted-mode work

## Confirmed Live Defects

Status note:

- `DR-1` and `DR-5` are now fixed in the current local worktree and remain listed here for traceability against the original remediation plan.

| ID   | Severity | Defect                                                                                            | Primary files                                                                                         |
| ---- | -------- | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| DR-1 | Critical | TypeScript `replay` mode still dispatches live network traffic                                    | `sdk/typescript/src/runtime.ts`, `sdk/typescript/src/interception.ts`                                 |
| DR-2 | High     | Session salt creation is only deterministic in-process, not cross-process                         | `internal/scrub/session_salt/manager.go`, `internal/store/store.go`, `internal/store/sqlite/store.go` |
| DR-3 | High     | Credit-card detector regex misses valid 13-digit cards and overmatches the documented range       | `internal/scrub/detectors/detectors.go`                                                               |
| DR-4 | Medium   | Phone detector boundary checks are not UTF-8 safe                                                 | `internal/scrub/detectors/detectors.go`                                                               |
| DR-5 | Medium   | TypeScript capture event model is not parity-complete with Python for streaming/tool-call traffic | `sdk/typescript/src/capture.ts`, `sdk/typescript/src/interception.ts`                                 |
| DR-6 | Medium   | Hybrid mode is referenced in architecture docs but unsupported in both SDKs                       | `sdk/python/stagehand/_runtime.py`, `sdk/typescript/src/runtime.ts`, docs                             |
| DR-7 | Medium   | OpenAI host detection is hardcoded to `api.openai.com`                                            | `sdk/python/stagehand/_openai.py`, TypeScript OpenAI classification paths                             |
| DR-8 | Low      | Scrub-rule normalization only lowercases `request.headers.*`                                      | `internal/scrub/rules.go`                                                                             |
| DR-9 | Low      | Persisted recording rebuilds the scrub pipeline per interaction                                   | `internal/recording/writer.go`                                                                        |

## Fix Order

### Phase 1: Safety And Determinism Blockers

Ship first:

1. `DR-1` TypeScript replay safety
2. `DR-2` Cross-process session salt determinism
3. `DR-3` and `DR-4` detector correctness

These are the highest-value fixes because they affect:

- whether replay is safe to trust
- whether scrubbed identifiers stay stable
- whether standard secret detection is actually correct

### Phase 2: Cross-SDK Behavior Gaps

Ship second:

4. `DR-5` TypeScript streaming parity
5. `DR-7` OpenAI host detection flexibility
6. `DR-6` hybrid mode decision and alignment

These do not corrupt persisted data, but they create product-surface drift and misleading behavior.

### Phase 3: Cleanup And Performance

Ship third:

7. `DR-8` broader header-rule normalization
8. `DR-9` scrub pipeline reuse

These are worth doing, but they should not block the higher-severity fixes.

## Detailed Remediation Plan

## DR-1: TypeScript Replay Must Not Hit Live APIs

- Severity: Critical
- Goal: `mode: "replay"` must never silently go live
- Estimate: `2d` for safety hotfix, `3-5d` for full exact replay
- Current status:
  TypeScript replay now fails closed on misses, exact-replays supported non-streaming and SSE interactions, and raises typed failures for timeout, error, and malformed seeded streaming interactions. The remaining boundary is scope, not safety: replay only supports the seeded interaction shapes the TypeScript SDK already captures.

### Immediate Decision

The shortest safe fix is:

- fail closed in TypeScript replay mode unless a replay store is installed

Do not keep the current behavior where replay mode still dispatches outbound requests.

### Implementation Steps

1. Extend the TypeScript runtime to carry replay context, not just capture context.
2. Add a replay store for exact matches, similar to the Python `OpenAIReplayStore`.
3. Make the interception layer branch on mode:
   - `record`: capture live traffic
   - `replay`: match and return a replayed response or raise a typed replay miss/failure
   - `passthrough`: no capture, no replay
4. Until exact replay exists for a request class, fail with a typed error before live dispatch.
5. Add explicit replay seeding from CLI-provided bundle input so `stagehand replay -- ...` works the same way for TS child processes as it does for Python.

### Code Targets

- `sdk/typescript/src/runtime.ts`
- `sdk/typescript/src/interception.ts`
- new module: `sdk/typescript/src/replay.ts`
- `sdk/typescript/src/index.ts`

### Acceptance Criteria

- TypeScript `replay` mode does not hit the network when no replay match exists.
- Exact replay can serve a stored successful interaction.
- Exact replay returns a controlled typed failure for stored error/timeout interactions.
- Replay misses are explicit and actionable.

### Tests To Add

- `sdk/typescript/src/replay.test.ts`
  - replay mode with no seed bundle fails closed before dispatch
  - replay of a matched success returns the expected status/body/headers
  - replay of a matched timeout raises typed replay failure
  - replay of a matched error raises typed replay failure
  - replay miss raises typed replay miss
  - server-hit counter remains `0` during replay tests

- `sdk/typescript/src/index.test.ts`
  - `init({ mode: 'replay' })` installs replay-safe behavior
  - replay seed import from env or explicit helper works

## DR-2: Session Salt Creation Must Be Cross-Process Safe

- Severity: High
- Goal: one session always resolves to one persisted salt, even across multiple processes
- Estimate: `2d`

### Current Gap

The keyed mutex in `manager.go` only serializes callers inside one process. Two Stagehand processes can still race through first salt creation for the same session.

### Implementation Steps

1. Add a store-level atomic create-if-absent operation for scrub salts.
2. Implement it in SQLite with one of:
   - `INSERT OR IGNORE` followed by `SELECT`
   - a transaction that guarantees first-write wins
3. Change `Manager.GetOrCreate()` to rely on store atomicity as the correctness mechanism.
4. Keep the keyed mutex only as an in-process optimization, or remove it if the store path is sufficient.

### Code Targets

- `internal/store/store.go`
- `internal/store/sqlite/store.go`
- `internal/scrub/session_salt/manager.go`

### Acceptance Criteria

- concurrent first-use from two managers backed by the same DB yields one `salt_id`
- same-session hashing stays stable across process boundaries
- no persisted salt is ever overwritten after first creation

### Tests To Add

- `internal/scrub/session_salt/manager_test.go`
  - existing concurrent single-process test stays
  - add two-manager same-DB test
  - add repeat `GetOrCreate()` after first create returns same material

- `internal/store/sqlite/store_test.go`
  - create-if-absent semantics test
  - duplicate create attempt does not overwrite first salt

- optional subprocess test:
  - `internal/scrub/session_salt/manager_integration_test.go`
  - two helper subprocesses race against one SQLite file
  - assert one stable stored `salt_id`

## DR-3: Credit-Card Detector Range Must Match The Spec

- Severity: High
- Goal: detect 13-19 digit cards correctly before Luhn filtering
- Estimate: `0.5d`

### Implementation Steps

1. Fix the regex so it represents 13-19 digits total, including separators.
2. Keep Luhn as the second-stage validator.
3. Re-run the detector corpus and add explicit boundary tests.

### Code Targets

- `internal/scrub/detectors/detectors.go`
- `internal/scrub/detectors/corpus_test.go`
- `internal/scrub/detectors/testdata/corpus.json`

### Acceptance Criteria

- valid 13-digit test card is detected
- valid 19-digit test card is detected
- 20-digit candidate is rejected
- non-Luhn sequences are rejected

### Tests To Add

- add positive cases:
  - valid 13-digit card
  - valid 16-digit card
  - valid 19-digit card

- add negative cases:
  - 12-digit number
  - 20-digit number
  - 16-digit non-Luhn sequence

## DR-4: Phone Detector Must Be UTF-8 Safe

- Severity: Medium
- Goal: boundary checks must work on Unicode text, not raw bytes
- Estimate: `0.5d`

### Implementation Steps

1. Replace byte-based boundary reads with rune-safe decoding using the surrounding substring.
2. Keep the existing digit-length logic.
3. Add Unicode-adjacent corpus cases.

### Code Targets

- `internal/scrub/detectors/detectors.go`
- `internal/scrub/detectors/corpus_test.go`

### Acceptance Criteria

- detector behavior is stable when phone-like text is adjacent to multibyte characters
- no false positive created by byte-splitting a Unicode rune

### Tests To Add

- positive:
  - phone number surrounded by spaces in ASCII text
  - phone number next to Unicode punctuation

- negative:
  - digits embedded in a Unicode word-like token
  - email-like text containing digit runs

## DR-5: TypeScript Event Model Must Reach Parity For Streaming

- Severity: Medium
- Goal: Python and TypeScript should not diverge on fundamental event shapes for supported flows
- Estimate: `3d`
- Current status:
  TypeScript-side streaming event emission is implemented, the shared cross-SDK parity suite covers a shared OpenAI SSE capture fixture with `stream_chunk`, `stream_end`, and tool-call boundary checks, and TypeScript streaming replay parity is now covered under `G4`. This is no longer an open capture-model defect in the current worktree.

### Implementation Steps

1. Expand `CapturedEventType` in TypeScript to include:
   - `stream_chunk`
   - `stream_end`
   - `tool_call_start`
   - `tool_call_end`
2. Add streaming capture logic for supported SSE flows.
3. Preserve chunk ordering and tool-call boundaries where the wire shape exposes them.
4. Extend the shared parity fixtures to cover one streaming success case.

### Code Targets

- `sdk/typescript/src/capture.ts`
- `sdk/typescript/src/interception.ts`
- `sdk/typescript/src/index.test.ts`
- `sdk/python/tests/test_parity.py`
- `testdata/sdk-parity/`

### Acceptance Criteria

- TypeScript can emit stream chunk and stream end events
- parity fixtures pass for a shared streaming fixture
- docs clearly state what still differs, if anything

### Tests To Add

- `sdk/typescript/src/index.test.ts`
  - SSE response emits ordered `stream_chunk` events
  - final `stream_end` is present

- parity fixtures:
  - `testdata/sdk-parity/openai-sse-success.json`
  - compare event ordering and selected event payload fields across Python and TS

## DR-6: Hybrid Mode Needs Either Implementation Or Removal

- Severity: Medium
- Goal: stop claiming a mode that the SDKs do not support
- Estimate: `0.5d` for docs-only alignment, `2-3d` for implementation

### Recommended Choice

Do not implement hybrid mode now unless a real design partner blocks on it.

The practical fix is:

- remove hybrid references from V1 user-facing docs
- keep hybrid as a future runtime concept, not a current SDK mode
- Current status:
  the SDKs still reject `hybrid`, and the user-facing docs now describe only `record`, `replay`, and `passthrough` as supported modes.

### Code Targets

- docs only, unless promoted:
  - `docs/stagehand-v1-build-plan.md`
  - `docs/stagehand-v1-epic-breakdown.md`
  - `README.md`
  - `ProductDocumentations/*`

### Acceptance Criteria

- no user-facing doc says hybrid mode exists in current SDK behavior
- SDK mode lists and docs agree

### Tests To Add

- none required if docs-only
- if implemented later:
  - replay-hit path uses stored response
  - replay-miss path records live response

## DR-7: OpenAI Host Detection Must Support Non-Default Hosts

- Severity: Medium
- Goal: OpenAI-aware capture/replay should work with Azure and custom base URLs
- Estimate: `1-2d`

### Implementation Steps

1. Change host detection from a single-host equality check to a configurable matcher.
2. Support:
   - `api.openai.com`
   - configured additional hosts
   - optionally path-based OpenAI classification for known gateway routes
3. Use `STAGEHAND_OPENAI_HOSTS` as the current SDK-level host allowlist surface.
4. Mirror the same behavior in TypeScript classification logic.

### Code Targets

- `sdk/python/stagehand/_openai.py`
- `sdk/python/stagehand/_httpx.py`
- `sdk/typescript/src/interception.ts`
- config/docs if host allowlist is configurable

### Acceptance Criteria

- custom-host OpenAI traffic can still be classified and replayed
- default `api.openai.com` behavior remains unchanged

### Tests To Add

- Python:
  - custom `base_url` request classified as OpenAI when host is configured
  - Azure-style path fixture

- TypeScript:
  - host classification test for configured OpenAI-compatible endpoint

## DR-8: Broaden Header Rule Normalization

- Severity: Low
- Goal: mixed-case header rules behave consistently for request and response headers
- Estimate: `0.5d`

### Implementation Steps

1. Extend `normalizeRulePattern()` beyond `request.headers.*`.
2. At minimum normalize:
   - `request.headers.*`
   - `response.headers.*`
3. Add tests for mixed-case request and response header patterns.

### Code Targets

- `internal/scrub/rules.go`
- `internal/scrub/rules_test.go`
- `internal/scrub/pipeline_test.go`

### Acceptance Criteria

- mixed-case request header rules work
- mixed-case response header rules work

### Tests To Add

- response header rule `response.headers.Authorization`
- lowercase and mixed-case response-header rules behave identically

## DR-9: Reuse Scrub Pipeline During Persisted Recording

- Severity: Low
- Goal: avoid recompiling the same rules for every interaction
- Estimate: `1d`

### Implementation Steps

1. Separate:
   - static compiled rule state
   - per-session salt/material state
2. Cache the compiled rule set in `recording.Writer`.
3. Rebuild only the salt-bound portion if necessary.
4. Add a micro-benchmark or allocation-count test if useful.

### Code Targets

- `internal/recording/writer.go`
- `internal/scrub/pipeline.go`

### Acceptance Criteria

- persisted recording does not recompile patterns per interaction
- behavior is unchanged

### Tests To Add

- unit test proving identical output before/after pipeline reuse
- optional benchmark:
  - `BenchmarkPersistInteractionWithReusedPipeline`

## Test-Suite Strengthening Plan

## Unit Tests

### Go

- `internal/scrub/detectors`
  - regex boundary cases
  - Unicode adjacency cases
  - corpus-based regression cases

- `internal/scrub/session_salt`
  - cross-manager same-DB determinism
  - first-write-wins semantics

- `internal/scrub`
  - mixed-case request and response header rule coverage

- `internal/recording`
  - writer behavior with cached pipeline path

### Python

- `sdk/python/tests`
  - custom OpenAI host classification
  - replay failure and replay miss behavior stays typed

### TypeScript

- `sdk/typescript/src`
  - replay-safe no-live-network tests
  - exact replay match/miss tests
  - streaming event-shape tests

## Integration Tests

- CLI replay of a TS child process in replay mode must not hit a live local HTTP server
- CLI replay of a Python child process with custom OpenAI host configuration
- persisted scrubbed identifiers remain stable across separate processes using one SQLite DB

## Concurrency Tests

- Go:
  - two managers, one DB, one session, concurrent first-use
  - optional subprocess race harness

- TypeScript:
  - replay store exact-match queue under parallel requests once replay exists

## Parity Tests

Extend the shared fixture suite under `testdata/sdk-parity/` with:

- one streaming success case
- one replay failure case
- one custom-host OpenAI case

Parity assertions should compare:

- event ordering
- canonical request shape
- terminal event shape
- scrub-report fields that are currently expected to match

## Regression Gate

Focused commands:

```powershell
go test ./internal/scrub ./internal/scrub/detectors ./internal/scrub/session_salt ./internal/recording -v
go test ./internal/store/sqlite -v
python -m pytest sdk/python/tests -q
npm run -w @stagehand/sdk test
```

Concurrency commands:

```powershell
go test -race ./internal/scrub/session_salt ./internal/store/sqlite
```

Full gate:

```powershell
npm run ci:all
```

## Definition Of Done

This remediation pass is done when:

- TypeScript `replay` mode no longer goes live
- one session resolves to one persisted salt even across processes
- credit-card detection matches the documented 13-19 digit range
- phone detection behaves correctly with Unicode-adjacent text
- TypeScript parity covers at least one streaming fixture
- docs no longer claim unsupported hybrid behavior in V1
- OpenAI-aware logic can handle configured non-default hosts
- mixed-case request and response header rules behave identically
- the strengthened unit, integration, concurrency, and parity tests all pass

## Recommended Execution Sequence

1. Ship `DR-1` as a safety fix first, even if the first step is fail-closed replay.
2. Ship `DR-2` next so scrub determinism is honest across processes.
3. Ship `DR-3` and `DR-4` together as a detector-correctness patch.
4. Ship `DR-5` and `DR-7` together because they both improve cross-SDK provider parity.
5. Resolve `DR-6` by either removing hybrid claims or promoting it to real work.
6. Finish `DR-8` and `DR-9` as cleanup/perf hardening.
