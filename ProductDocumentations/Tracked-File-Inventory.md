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

| File                          | Purpose                                 | Current Status                                  |
| ----------------------------- | --------------------------------------- | ----------------------------------------------- |
| `cmd/stagehand/main.go`       | CLI binary entrypoint                   | Implemented as scaffold with tested output path |
| `cmd/stagehand/main_test.go`  | Tests for the CLI binary output path    | Implemented                                     |
| `cmd/stagehandd/main.go`      | Local daemon binary entrypoint          | Implemented as scaffold with tested output path |
| `cmd/stagehandd/main_test.go` | Tests for the daemon binary output path | Implemented                                     |

## Go Internal Packages

### Active package files

| File                               | Purpose                                                           | Current Status |
| ---------------------------------- | ----------------------------------------------------------------- | -------------- |
| `internal/config/doc.go`           | Package marker for config package                                 | Implemented    |
| `internal/config/config.go`        | Config schema definitions, defaults, YAML loading, and validation | Implemented    |
| `internal/config/config_test.go`   | Tests for config defaults, loading, and validation rules          | Implemented    |
| `internal/version/version.go`      | Shared artifact version constants and CLI message formatter       | Implemented    |
| `internal/version/version_test.go` | Tests for version helper behavior                                 | Implemented    |
| `internal/analysis/doc.go`         | Package marker for analysis package                               | Scaffold only  |
| `internal/recorder/doc.go`         | Package marker for recorder package                               | Scaffold only  |
| `internal/runtime/doc.go`          | Package marker for runtime package                                | Scaffold only  |
| `internal/services/doc.go`         | Package marker for services package                               | Scaffold only  |
| `internal/store/doc.go`            | Package marker for store package                                  | Scaffold only  |

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
- `internal/runtime/replay`
- `internal/runtime/snapshots`
- `internal/scrub/detectors`
- `internal/scrub/rules`
- `internal/scrub/session_salt`
- `internal/sessions`
- `internal/services/anthropic`
- `internal/services/generic_http`
- `internal/services/gmail`
- `internal/services/openai`
- `internal/services/postgres`
- `internal/services/slack`
- `internal/services/stripe`
- `internal/store/postgres`
- `internal/store/sqlite`

## Python SDK Files

| File                               | Purpose                                                           | Current Status          |
| ---------------------------------- | ----------------------------------------------------------------- | ----------------------- |
| `sdk/python/README.md`             | Python SDK package note                                           | Implemented             |
| `sdk/python/pyproject.toml`        | Python package metadata, dev dependencies, pytest and Ruff config | Implemented             |
| `sdk/python/stagehand/__init__.py` | Python SDK package entrypoint with placeholder `init()`           | Implemented as scaffold |
| `sdk/python/stagehand/_version.py` | Python SDK package and artifact version placeholders              | Implemented             |
| `sdk/python/tests/__init__.py`     | Python test package marker                                        | Implemented             |
| `sdk/python/tests/test_smoke.py`   | Python smoke tests for version and placeholder init behavior      | Implemented             |

## TypeScript SDK Files

| File                               | Purpose                                                                | Current Status          |
| ---------------------------------- | ---------------------------------------------------------------------- | ----------------------- |
| `sdk/typescript/README.md`         | TypeScript SDK package note                                            | Implemented             |
| `sdk/typescript/package.json`      | TypeScript package metadata, exports, and scripts                      | Implemented             |
| `sdk/typescript/tsconfig.json`     | TypeScript compiler configuration                                      | Implemented             |
| `sdk/typescript/src/index.ts`      | TypeScript SDK entrypoint with placeholder `init()` and exported types | Implemented as scaffold |
| `sdk/typescript/src/index.test.ts` | TypeScript smoke test for placeholder `init()` behavior                | Implemented             |
| `sdk/typescript/src/version.ts`    | TypeScript SDK and artifact version placeholders                       | Implemented             |

## Dashboard and Examples

| File                                          | Purpose                                                      | Current Status |
| --------------------------------------------- | ------------------------------------------------------------ | -------------- |
| `dashboard/README.md`                         | Placeholder note that hosted dashboard is deferred in Plan A | Implemented    |
| `examples/onboarding-agent/README.md`         | Planned onboarding example description                       | Scaffold only  |
| `examples/refund-agent/README.md`             | Planned refund example description                           | Scaffold only  |
| `examples/support-escalation-agent/README.md` | Planned support escalation example description               | Scaffold only  |

## Scripts

| File                          | Purpose                                               | Current Status |
| ----------------------------- | ----------------------------------------------------- | -------------- |
| `scripts/check-go-format.mjs` | Root Go formatting check used by local scripts and CI | Implemented    |

## Planning and Product Docs Under `docs/`

| File                                      | Purpose                                                               | Current Status |
| ----------------------------------------- | --------------------------------------------------------------------- | -------------- |
| `docs/stagehand-v1-build-plan.md`         | Main build plan, scope cuts, milestones, and execution guidance       | Implemented    |
| `docs/stagehand-v1-epic-milestone-map.md` | Epic-to-milestone mapping and blocker rules                           | Implemented    |
| `docs/stagehand-v1-epic-breakdown.md`     | Story-level backlog and epic checklists                               | Implemented    |
| `docs/config-schema.md`                   | Human-readable reference for `stagehand.yml` and `stagehand.test.yml` | Implemented    |

## Product Documentation Files Under `ProductDocumentations/`

| File                                                          | Purpose                                             | Current Status |
| ------------------------------------------------------------- | --------------------------------------------------- | -------------- |
| `ProductDocumentations/README.md`                             | Index for the current product documentation set     | Implemented    |
| `ProductDocumentations/Tracked-File-Inventory.md`             | This file                                           | Implemented    |
| `ProductDocumentations/Code-Modules-Types-and-EntryPoints.md` | Detailed code and type reference                    | Implemented    |
| `ProductDocumentations/Work-Completed-and-Current-State.md`   | Completed work log and current implementation state | Implemented    |
