# Code Modules, Types, and Entrypoints

## Purpose

This document describes the code that currently exists in the Stagehand repository.

Important constraint:

- the current codebase is still early-stage
- much of the repo is structural scaffolding
- there are very few "classes" in the object-oriented sense

The implemented code currently uses:

- Go packages
- Go structs, constants, and functions
- Python module-level exports
- TypeScript module-level exports and type aliases

## Go Code

## Package `stagehand/internal/version`

### File: `internal/version/version.go`

Purpose:

- defines canonical version constants used by the current Go binaries
- centralizes the CLI/daemon message formatter

### Constants

| Name              | Type     | Meaning                                                            |
| ----------------- | -------- | ------------------------------------------------------------------ |
| `ArtifactVersion` | `string` | Current root artifact version placeholder                          |
| `CLIVersion`      | `string` | Current CLI version constant, currently equal to `ArtifactVersion` |

### Functions

| Function     | Signature                                     | Purpose                                                         |
| ------------ | --------------------------------------------- | --------------------------------------------------------------- |
| `CLIMessage` | `func CLIMessage(binary, note string) string` | Builds the formatted message printed by the current Go binaries |

### Tests

File: `internal/version/version_test.go`

Covered behavior:

- `CLIMessage()` returns the expected output
- `ArtifactVersion` and `CLIVersion` match

## Package `stagehand/cmd/stagehand`

### File: `cmd/stagehand/main.go`

Purpose:

- current CLI binary entrypoint
- uses `run(io.Writer)` so output is testable

### Functions

| Function | Signature                     | Purpose                                                                  |
| -------- | ----------------------------- | ------------------------------------------------------------------------ |
| `main`   | `func main()`                 | Writes CLI output to `os.Stdout` and exits with failure if writing fails |
| `run`    | `func run(w io.Writer) error` | Core testable output path for the CLI scaffold                           |

### Tests

File: `cmd/stagehand/main_test.go`

Covered behavior:

- `run()` writes the expected scaffold message

## Package `stagehand/cmd/stagehandd`

### File: `cmd/stagehandd/main.go`

Purpose:

- current local daemon binary entrypoint
- mirrors the CLI structure with a testable `run(io.Writer)` function

### Functions

| Function | Signature                     | Purpose                                                                     |
| -------- | ----------------------------- | --------------------------------------------------------------------------- |
| `main`   | `func main()`                 | Writes daemon output to `os.Stdout` and exits with failure if writing fails |
| `run`    | `func run(w io.Writer) error` | Core testable output path for the daemon scaffold                           |

### Tests

File: `cmd/stagehandd/main_test.go`

Covered behavior:

- `run()` writes the expected daemon scaffold message

## Package `stagehand/internal/config`

### File: `internal/config/config.go`

Purpose:

- defines the current config schema for `stagehand.yml` and `stagehand.test.yml`
- provides defaults
- loads YAML from disk
- validates config shape and contradictory settings
- aggregates validation errors

### Schema/version constant

| Name            | Type     | Meaning                                   |
| --------------- | -------- | ----------------------------------------- |
| `SchemaVersion` | `string` | Current config schema version, `v1alpha1` |

### Enum-like type aliases

| Name            | Underlying Type | Role                                                  |
| --------------- | --------------- | ----------------------------------------------------- |
| `Mode`          | `string`        | Runtime mode for record/replay/passthrough            |
| `ReplayMode`    | `string`        | Replay strategy, currently `exact` or `prompt_replay` |
| `ClockMode`     | `string`        | Replay clock mode, currently `wall` only              |
| `FallbackTier`  | `string`        | Allowed fallback tiers                                |
| `AuthMode`      | `string`        | Auth resolver behavior                                |
| `ReportFormat`  | `string`        | CI/report output format                               |
| `FailCondition` | `string`        | CI failure condition                                  |

### Constant values

#### `Mode`

- `ModeRecord`
- `ModeReplay`
- `ModePassthrough`

#### `ReplayMode`

- `ReplayModeExact`
- `ReplayModePromptReplay`

#### `ClockMode`

- `ClockModeWall`

#### `FallbackTier`

- `FallbackTierExact`
- `FallbackTierNearestNeighbor`
- `FallbackTierStateSynthesis`
- `FallbackTierLLMSynthesis`

#### `AuthMode`

- `AuthModePermissive`
- `AuthModeStrictRecorded`
- `AuthModeStrictMatch`

#### `ReportFormat`

- `ReportFormatTerminal`
- `ReportFormatJSON`
- `ReportFormatGitHubMarkdown`

#### `FailCondition`

- `FailConditionBehaviorDiff`
- `FailConditionAssertionFailure`
- `FailConditionFallbackRegression`

### Structs and their purposes

| Struct                 | Purpose                                                  |
| ---------------------- | -------------------------------------------------------- |
| `Config`               | Top-level runtime config model for `stagehand.yml`       |
| `RecordConfig`         | Record-time defaults                                     |
| `CaptureConfig`        | Capture-specific options under `record.capture`          |
| `ReplayConfig`         | Replay-specific runtime options                          |
| `ScrubConfig`          | Scrub policy section                                     |
| `DetectorConfig`       | Boolean toggles for scrub detectors                      |
| `FallbackConfig`       | Fallback policy section                                  |
| `LLMSynthesisConfig`   | Tier-3 fallback configuration                            |
| `AuthConfig`           | Default and per-service auth mode configuration          |
| `TestConfig`           | Top-level test-run config model for `stagehand.test.yml` |
| `BaselineConfig`       | Baseline branch/requirement settings                     |
| `TestReplayConfig`     | Test replay restrictions                                 |
| `TestFallbackConfig`   | Disallowed fallback tiers for tests                      |
| `ErrorInjectionConfig` | Error injection section                                  |
| `ErrorInjectionRule`   | One error injection rule                                 |
| `ErrorMatch`           | Rule matching conditions                                 |
| `ErrorInject`          | Rule injection payload                                   |
| `CIConfig`             | CI reporting and failure behavior                        |
| `ValidationError`      | Aggregated validation error type                         |

### Exported functions and methods

| Function or Method        | Signature                                        | Purpose                                  |
| ------------------------- | ------------------------------------------------ | ---------------------------------------- |
| `DefaultConfig`           | `func DefaultConfig() Config`                    | Returns default runtime config           |
| `DefaultTestConfig`       | `func DefaultTestConfig() TestConfig`            | Returns default test config              |
| `Load`                    | `func Load(path string) (Config, error)`         | Loads and validates `stagehand.yml`      |
| `LoadTest`                | `func LoadTest(path string) (TestConfig, error)` | Loads and validates `stagehand.test.yml` |
| `(Config) Validate`       | `func (c Config) Validate() error`               | Validates runtime config                 |
| `(TestConfig) Validate`   | `func (c TestConfig) Validate() error`           | Validates test config                    |
| `(ValidationError) Error` | `func (e *ValidationError) Error() string`       | Returns aggregated validation message    |

### Internal helpers

These are package-private implementation helpers, not public API:

- `loadYAML`
- `validateTierSequence`
- `validateTierSet`
- `validateErrorRule`
- `(*ValidationError).add`
- `(*ValidationError).err`

### Tests

File: `internal/config/config_test.go`

Covered behavior:

- default runtime config is valid
- default test config is intentionally incomplete until `session` is set
- defaults are applied when partial config is loaded
- invalid runtime config returns aggregated validation errors
- valid test config loads correctly
- contradictory replay settings are rejected

## Go Package Markers

The following files currently exist only to claim package names and document planned purpose:

| File                       | Package Purpose           | Current Implementation Depth              |
| -------------------------- | ------------------------- | ----------------------------------------- |
| `internal/analysis/doc.go` | analysis package          | marker only                               |
| `internal/recorder/doc.go` | recorder package          | marker only                               |
| `internal/runtime/doc.go`  | runtime package           | marker only                               |
| `internal/services/doc.go` | service simulator package | marker only                               |
| `internal/store/doc.go`    | store package             | marker only                               |
| `internal/config/doc.go`   | config package            | marker only, real logic is in `config.go` |

## Python Code

## Module `sdk/python/stagehand/__init__.py`

Purpose:

- current Python SDK package entrypoint
- exposes package version and placeholder `init()`

### Exports

| Export             | Kind     | Purpose                                   |
| ------------------ | -------- | ----------------------------------------- |
| `__version__`      | string   | Python package version                    |
| `ARTIFACT_VERSION` | string   | Artifact version placeholder              |
| `init`             | function | Placeholder Stagehand SDK init entrypoint |

### Behavior

- `init()` currently raises `NotImplementedError`
- this is intentional until Story D1 is implemented

## Module `sdk/python/stagehand/_version.py`

### Constants

| Name               | Meaning                           |
| ------------------ | --------------------------------- |
| `PACKAGE_VERSION`  | Python package version            |
| `ARTIFACT_VERSION` | Root artifact version placeholder |
| `__version__`      | Alias to `PACKAGE_VERSION`        |

## Python tests

File: `sdk/python/tests/test_smoke.py`

Covered behavior:

- package version is defined
- `init()` raises `NotImplementedError`

## TypeScript Code

## Module `sdk/typescript/src/version.ts`

### Exports

| Export             | Kind     | Purpose                      |
| ------------------ | -------- | ---------------------------- |
| `SDK_VERSION`      | constant | TypeScript SDK version       |
| `ARTIFACT_VERSION` | constant | Artifact version placeholder |

## Module `sdk/typescript/src/index.ts`

Purpose:

- current TypeScript SDK entrypoint
- exports version constants, types, and placeholder `init()`

### Type exports

| Name            | Kind        | Purpose                                                |
| --------------- | ----------- | ------------------------------------------------------ |
| `StagehandMode` | union type  | Allowed mode values: `record`, `replay`, `passthrough` |
| `InitOptions`   | object type | Input shape for `init()`                               |

### Function exports

| Name   | Signature                                     | Purpose                                               |
| ------ | --------------------------------------------- | ----------------------------------------------------- |
| `init` | `function init(_options: InitOptions): never` | Placeholder SDK init entrypoint that currently throws |

### Behavior

- `init()` currently throws an error mentioning Story G1

## TypeScript tests

File: `sdk/typescript/src/index.test.ts`

Covered behavior:

- `init()` throws until Story G1 is implemented

## Scripts and Workflow Enforcement

## Root task runner: `package.json`

Purpose:

- central local and CI entrypoint for format, build, lint, and test commands

Important current commands:

- `build:go:cli`
- `build:go:daemon`
- `build:go:all`
- `build:python`
- `build:typescript`
- `build:all`
- `ci:format`
- `ci:build`
- `ci:lint`
- `ci:test`
- `ci:all`

## Script: `scripts/check-go-format.mjs`

Purpose:

- runs `gofmt -l .`
- fails if any Go file needs formatting
- is used by local scripts and CI

## Workflow: `.github/workflows/ci.yml`

Purpose:

- runs separate Go, Python, and TypeScript jobs
- enforces formatting
- enforces Go build, vet, and tests
- enforces Python Ruff and pytest
- enforces TypeScript compile-time linting and smoke tests

## What Does Not Exist Yet

These items are planned in the repo layout but do not have implementation code yet:

- recorder logic
- runtime/session logic
- scrub engine logic
- store implementations
- diff engine
- assertion engine
- simulator implementations
- dashboard application
- real SDK interception logic

This distinction matters because the repo now has a stable shape, but only a subset of the planned system has actual behavior.
