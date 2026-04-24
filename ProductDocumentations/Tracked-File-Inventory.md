# Tracked File Inventory

## Purpose

This document inventories the tracked files that currently define the Stagehand repository.

Excluded from this inventory:

- `node_modules/`
- `bin/`
- Python build artifacts such as `dist/` and `*.egg-info`
- local caches such as `.ruff_cache/` and `.pytest_cache/`

## Root Files

| File                 | Purpose                                                                              | Current Status |
| -------------------- | ------------------------------------------------------------------------------------ | -------------- |
| `README.md`          | Root repository overview, local checks, build commands, version placeholder pointers | Implemented    |
| `VERSION`            | Root artifact version placeholder                                                    | Implemented    |
| `go.mod`             | Go module declaration and direct dependencies                                        | Implemented    |
| `go.sum`             | Go dependency checksums                                                              | Implemented    |
| `package.json`       | Root task runner for format, build, lint, and test commands                          | Implemented    |
| `package-lock.json`  | Locked Node dependency graph for reproducible installs                               | Implemented    |
| `.gitignore`         | Ignore rules for generated and local-only files                                      | Implemented    |
| `.editorconfig`      | Cross-language editor defaults                                                       | Implemented    |
| `.prettierrc.json`   | Prettier configuration                                                               | Implemented    |
| `.prettierignore`    | Prettier ignore rules                                                                | Implemented    |
| `stagehand.yml`      | Sample runtime configuration file                                                    | Implemented    |
| `stagehand.test.yml` | Sample test-run configuration file                                                   | Implemented    |

## GitHub Workflow Files

| File                          | Purpose                                                                  | Current Status |
| ----------------------------- | ------------------------------------------------------------------------ | -------------- |
| `.github/workflows/ci.yml`    | CI workflow for Go, Python, and TypeScript format, build, lint, and test | Implemented    |
| `.github/workflows/README.md` | Placeholder note for workflow area                                       | Implemented    |

## Go Command Entrypoints

| File                            | Purpose                                                                                | Current Status                                  |
| ------------------------------- | -------------------------------------------------------------------------------------- | ----------------------------------------------- |
| `cmd/stagehand/main.go`         | CLI binary entrypoint with `record`, `replay`, and `inspect` commands                  | Implemented                                     |
| `cmd/stagehand/inspect.go`      | Terminal inspect renderer and run-loading helpers for stored runs                      | Implemented                                     |
| `cmd/stagehand/workflow.go`     | Shared managed-command, capture-bundle, and writer helpers for CLI record/replay flows | Implemented                                     |
| `cmd/stagehand/main_test.go`    | Tests for CLI help, record flow, and replay flow                                       | Implemented                                     |
| `cmd/stagehand/inspect_test.go` | Tests for inspect selector validation, nested rendering, and failed-run inspection     | Implemented                                     |
| `cmd/stagehandd/main.go`        | Local daemon binary entrypoint                                                         | Implemented as scaffold with tested output path |
| `cmd/stagehandd/main_test.go`   | Tests for the daemon binary output path                                                | Implemented                                     |

## Go Internal Packages

### Active package files

| File                                          | Purpose                                                                    | Current Status |
| --------------------------------------------- | -------------------------------------------------------------------------- | -------------- |
| `internal/config/doc.go`                      | Package marker for config package                                          | Implemented    |
| `internal/config/config.go`                   | Config schema definitions, defaults, YAML loading, and validation          | Implemented    |
| `internal/config/config_test.go`              | Tests for config defaults, loading, and validation rules                   | Implemented    |
| `internal/config/fixture_test.go`             | Fixture-driven tests for config schema validation                          | Implemented    |
| `internal/recording/writer.go`                | Recorder-side safe persisted writer that scrubs before store writes        | Implemented    |
| `internal/recording/writer_test.go`           | End-to-end tests for scrub-before-persist behavior and raw DB safety       | Implemented    |
| `internal/runtime/replay/exact.go`            | Exact-replay eligibility check and machine-readable replay result builder  | Implemented    |
| `internal/runtime/replay/exact_test.go`       | Direct tests for exact-replay eligibility and replay result summarization  | Implemented    |
| `internal/recorder/doc.go`                    | Package marker for recorder package                                        | Implemented    |
| `internal/recorder/schema.go`                 | Recording artifact schema types and validation                             | Implemented    |
| `internal/recorder/schema_test.go`            | Tests for artifact validation and JSON round-trip behavior                 | Implemented    |
| `internal/recorder/fixture_test.go`           | Fixture-driven tests for artifact schema validation                        | Implemented    |
| `internal/scrub/doc.go`                       | Package marker for structural scrub package                                | Implemented    |
| `internal/scrub/pipeline.go`                  | Structural plus detector-driven scrub pipeline and scrub report generation | Implemented    |
| `internal/scrub/pipeline_test.go`             | Tests for structural rules, detector mutation, and scrub report behavior   | Implemented    |
| `internal/scrub/rules.go`                     | Custom-rule merge and conflict validation helpers                          | Implemented    |
| `internal/scrub/rules_test.go`                | Tests for default/custom rule merging and conflict behavior                | Implemented    |
| `internal/scrub/detectors/doc.go`             | Package marker for detector library package                                | Implemented    |
| `internal/scrub/detectors/detectors.go`       | Free-text detector kinds, configurable libraries, and validation helpers   | Implemented    |
| `internal/scrub/detectors/corpus_test.go`     | Corpus-driven tests for detector coverage and match ordering               | Implemented    |
| `internal/scrub/session_salt/doc.go`          | Package marker for session-salt manager package                            | Implemented    |
| `internal/scrub/session_salt/manager.go`      | Session salt generation, encryption, and deterministic replacements        | Implemented    |
| `internal/scrub/session_salt/manager_test.go` | Tests for encrypted salt persistence and replacement stability             | Implemented    |
| `internal/store/doc.go`                       | Package marker for store package                                           | Implemented    |
| `internal/store/store.go`                     | Shared local store interface and store data models                         | Implemented    |
| `internal/store/store_test.go`                | Unit tests for store lifecycle and artifact conversion rules               | Implemented    |
| `internal/store/contracttest/contracttest.go` | Reusable backend-agnostic artifact store contract test suite               | Implemented    |
| `internal/store/sqlite/doc.go`                | Package marker for SQLite local store package                              | Implemented    |
| `internal/store/sqlite/migrate.go`            | SQLite open helpers and migration runner                                   | Implemented    |
| `internal/store/sqlite/migrate_test.go`       | Tests for SQLite migration runner and initial schema                       | Implemented    |
| `internal/store/sqlite/contract_test.go`      | SQLite runner for the shared artifact store contract suite                 | Implemented    |
| `internal/store/sqlite/store.go`              | SQLite implementation of the artifact store interface                      | Implemented    |
| `internal/store/sqlite/store_test.go`         | Tests for run, interaction, salt, and baseline store behavior              | Implemented    |
| `internal/version/version.go`                 | Shared artifact version constants and CLI message formatter                | Implemented    |
| `internal/version/version_test.go`            | Tests for version helper behavior                                          | Implemented    |
| `internal/analysis/doc.go`                    | Package marker for analysis package                                        | Scaffold only  |
| `internal/runtime/doc.go`                     | Package marker for runtime package                                         | Scaffold only  |
| `internal/services/doc.go`                    | Package marker for services package                                        | Scaffold only  |

### Empty but planned directories

These directories exist to stabilize repo layout but do not yet contain implementation files:

- `internal/analysis/assertions`
- `internal/analysis/diff`
- `internal/ci`
- `internal/classify`
- `internal/conformance`
- `internal/graph`
- `internal/runtime/clock`
- `internal/runtime/errorinject`
- `internal/runtime/fallback`
- `internal/runtime/queue`
- `internal/runtime/snapshots`
- `internal/scrub/rules`
- `internal/sessions`
- `internal/services/anthropic`
- `internal/services/generic_http`
- `internal/services/gmail`
- `internal/services/openai`
- `internal/services/postgres`
- `internal/services/slack`
- `internal/services/stripe`
- `internal/store/postgres`

## Python SDK Files

| File                                          | Purpose                                                                                    | Current Status |
| --------------------------------------------- | ------------------------------------------------------------------------------------------ | -------------- |
| `sdk/python/README.md`                        | Python SDK package note and current env-driven workflow summary                            | Implemented    |
| `sdk/python/pyproject.toml`                   | Python package metadata, dev dependencies, pytest and Ruff config                          | Implemented    |
| `sdk/python/stagehand/__init__.py`            | Python SDK package entrypoint exporting bootstrap/runtime APIs                             | Implemented    |
| `sdk/python/stagehand/_capture.py`            | Python in-memory interaction normalization and capture buffer                              | Implemented    |
| `sdk/python/stagehand/_httpx.py`              | Python `httpx` sync and async interception patching                                        | Implemented    |
| `sdk/python/stagehand/_openai.py`             | Python OpenAI request detection and exact replay matching                                  | Implemented    |
| `sdk/python/stagehand/_runtime.py`            | Python bootstrap runtime singleton, metadata, env wiring, and bundle export/import helpers | Implemented    |
| `sdk/python/stagehand/_version.py`            | Python SDK package and artifact version placeholders                                       | Implemented    |
| `sdk/python/tests/conftest.py`                | Test fixture resetting Python bootstrap singleton between tests                            | Implemented    |
| `sdk/python/tests/__init__.py`                | Python test package marker                                                                 | Implemented    |
| `sdk/python/tests/test_httpx_interception.py` | Python tests for sync, async, timeout, error interception, and bundle export/replay wiring | Implemented    |
| `sdk/python/tests/test_smoke.py`              | Python tests for bootstrap init behavior, runtime access, and env bootstrap                | Implemented    |

## TypeScript SDK Files

| File                                 | Purpose                                                                              | Current Status |
| ------------------------------------ | ------------------------------------------------------------------------------------ | -------------- |
| `sdk/typescript/README.md`           | TypeScript SDK package note and current bootstrap/interception status                | Implemented    |
| `sdk/typescript/package.json`        | TypeScript package metadata, exports, and scripts                                    | Implemented    |
| `sdk/typescript/tsconfig.json`       | TypeScript compiler configuration                                                    | Implemented    |
| `sdk/typescript/src/capture.ts`      | TypeScript artifact-shaped in-memory interaction capture buffer                      | Implemented    |
| `sdk/typescript/src/interception.ts` | `undici` dispatcher interception for Node HTTP traffic                               | Implemented    |
| `sdk/typescript/src/index.ts`        | TypeScript SDK entrypoint exporting the bootstrap/runtime public API                 | Implemented    |
| `sdk/typescript/src/index.test.ts`   | TypeScript tests for bootstrap, `fetch`, `undici`, error, and timeout capture        | Implemented    |
| `sdk/typescript/src/runtime.ts`      | TypeScript bootstrap singleton, metadata, env wiring, init logic, and capture access | Implemented    |
| `sdk/typescript/src/version.ts`      | TypeScript SDK and artifact version placeholders                                     | Implemented    |

## Dashboard and Examples

| File                                             | Purpose                                                          | Current Status |
| ------------------------------------------------ | ---------------------------------------------------------------- | -------------- |
| `dashboard/README.md`                            | Placeholder note that hosted dashboard is deferred in Plan A     | Implemented    |
| `examples/onboarding-agent/README.md`            | Onboarding example scope and D3 usage note                       | Implemented    |
| `examples/onboarding-agent/openai_demo_agent.py` | Minimal streaming OpenAI example agent used by integration tests | Implemented    |
| `examples/refund-agent/README.md`                | Planned refund example description                               | Scaffold only  |
| `examples/support-escalation-agent/README.md`    | Planned support escalation example description                   | Scaffold only  |

## Scripts

| File                          | Purpose                                               | Current Status |
| ----------------------------- | ----------------------------------------------------- | -------------- |
| `scripts/check-go-format.mjs` | Root Go formatting check used by local scripts and CI | Implemented    |

## Schema Fixture Files

| File                                                                          | Purpose                                                          | Current Status |
| ----------------------------------------------------------------------------- | ---------------------------------------------------------------- | -------------- |
| `internal/config/testdata/runtime-valid-minimal.stagehand.yml`                | Valid runtime config schema fixture                              | Implemented    |
| `internal/config/testdata/runtime-invalid-unknown-field.stagehand.yml`        | Invalid runtime config fixture for strict-field decode           | Implemented    |
| `internal/config/testdata/runtime-invalid-schema-version.stagehand.yml`       | Invalid runtime config fixture for version mismatch              | Implemented    |
| `internal/config/testdata/runtime-valid-custom-rules.stagehand.yml`           | Valid runtime config fixture for custom scrub rules              | Implemented    |
| `internal/config/testdata/runtime-invalid-custom-rule-conflict.stagehand.yml` | Invalid runtime config fixture for built-in scrub-rule conflicts | Implemented    |
| `internal/config/testdata/test-valid-minimal.stagehand.test.yml`              | Valid test config schema fixture                                 | Implemented    |
| `internal/config/testdata/test-invalid-replay-conflict.stagehand.test.yml`    | Invalid test config fixture for replay conflict                  | Implemented    |
| `internal/config/testdata/test-invalid-duplicate-fallback.stagehand.test.yml` | Invalid test config fixture for duplicate tiers                  | Implemented    |
| `internal/recorder/testdata/valid-run.json`                                   | Valid artifact schema fixture                                    | Implemented    |
| `internal/recorder/testdata/invalid-unknown-field.json`                       | Invalid artifact fixture for strict JSON decode                  | Implemented    |
| `internal/recorder/testdata/invalid-corrupted-without-issues.json`            | Invalid artifact fixture for corrupted-state rules               | Implemented    |
| `internal/recorder/testdata/invalid-schema-version.json`                      | Invalid artifact fixture for version mismatch                    | Implemented    |
| `internal/store/sqlite/migrations/0001_initial.sql`                           | Initial SQLite schema migration                                  | Implemented    |
| `internal/store/sqlite/migrations/0002_add_run_integrity_issues.sql`          | Adds persisted run integrity issues to the runs table            | Implemented    |
| `internal/scrub/detectors/testdata/corpus.json`                               | Positive and negative detector corpus fixture                    | Implemented    |

## Planning and Product Docs Under `docs/`

| File                                      | Purpose                                                               | Current Status |
| ----------------------------------------- | --------------------------------------------------------------------- | -------------- |
| `docs/stagehand-v1-build-plan.md`         | Main build plan, scope cuts, milestones, and execution guidance       | Implemented    |
| `docs/stagehand-v1-epic-milestone-map.md` | Epic-to-milestone mapping and blocker rules                           | Implemented    |
| `docs/stagehand-v1-epic-breakdown.md`     | Story-level backlog and epic checklists                               | Implemented    |
| `docs/config-schema.md`                   | Human-readable reference for `stagehand.yml` and `stagehand.test.yml` | Implemented    |
| `docs/artifact-schema.md`                 | Human-readable reference for recording artifact schemas               | Implemented    |
| `docs/schema-compatibility.md`            | Validation strategy and schema compatibility policy                   | Implemented    |
| `docs/scrub-structural-pipeline.md`       | Human-readable reference for the E1 structural scrub pipeline         | Implemented    |
| `docs/scrub-detector-library.md`          | Human-readable reference for the E2 free-text detector library        | Implemented    |
| `docs/scrub-session-hashing.md`           | Human-readable reference for the E3 session-salt and hashing logic    | Implemented    |
| `docs/sqlite-local-store.md`              | Human-readable reference for local SQLite schema and migration policy | Implemented    |

## Product Documentation Files Under `ProductDocumentations/`

| File                                                          | Purpose                                             | Current Status |
| ------------------------------------------------------------- | --------------------------------------------------- | -------------- |
| `ProductDocumentations/README.md`                             | Index for the current product documentation set     | Implemented    |
| `ProductDocumentations/Tracked-File-Inventory.md`             | This file                                           | Implemented    |
| `ProductDocumentations/Code-Modules-Types-and-EntryPoints.md` | Detailed code and type reference                    | Implemented    |
| `ProductDocumentations/Work-Completed-and-Current-State.md`   | Completed work log and current implementation state | Implemented    |
