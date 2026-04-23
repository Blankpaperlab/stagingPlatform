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
- implements root help plus managed `record`, `replay`, and `inspect` command dispatch
- uses a testable `run(args, stdout, stderr)` path

### Functions

| Function             | Signature                                                                                                                 | Purpose                                                                             |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `main`               | `func main()`                                                                                                             | Executes the CLI with process args and exits non-zero on failure                    |
| `run`                | `func run(args []string, stdout io.Writer, stderr io.Writer) error`                                                       | Parses the root command and dispatches subcommands                                  |
| `runRecord`          | `func runRecord(args []string, stdout io.Writer, _ io.Writer) (runErr error)`                                             | Runs a managed subprocess, imports its capture bundle, and persists a recording run |
| `runReplay`          | `func runReplay(args []string, stdout io.Writer, _ io.Writer) error`                                                      | Loads a stored run, seeds a managed subprocess, and persists a replay run           |
| `inspectHelpText`    | `func inspectHelpText() string`                                                                                           | Returns help text for the `inspect` subcommand                                      |
| `finalizeRun`        | `func finalizeRun(ctx context.Context, artifactStore store.ArtifactStore, runRecord store.RunRecord, runErr error) error` | Marks a run `complete` or `corrupted` cleanly                                       |
| `newRunRecord`       | `func newRunRecord(session string, cfg config.Config) (store.RunRecord, error)`                                           | Builds a new `running` run record for CLI workflows                                 |
| `generateRunID`      | `func generateRunID() (string, error)`                                                                                    | Generates a random run identifier                                                   |
| `sqliteDatabasePath` | `func sqliteDatabasePath(storagePath string) string`                                                                      | Resolves the SQLite file path from the configured storage path                      |
| `writeRootHelp`      | `func writeRootHelp(w io.Writer)`                                                                                         | Writes root CLI help                                                                |
| `rootHelpText`       | `func rootHelpText() string`                                                                                              | Returns root help text                                                              |
| `recordHelpText`     | `func recordHelpText() string`                                                                                            | Returns help text for the `record` subcommand                                       |
| `replayHelpText`     | `func replayHelpText() string`                                                                                            | Returns help text for the `replay` subcommand                                       |
| `loadReplayRun`      | `func loadReplayRun(ctx context.Context, artifactStore store.ArtifactStore, runID, session string) (recorder.Run, error)` | Loads one replay source by run ID or the latest replayable stored run for a session |

### Tests

File: `cmd/stagehand/main_test.go`

Covered behavior:

- root help is shown when no command is provided
- `record` rejects missing required session input
- `record` rejects a missing managed command
- `record` runs a managed helper process, imports its capture bundle, persists scrubbed interactions, and emits JSON
- `replay` rejects invalid selector combinations
- `replay` rejects a missing managed command
- `replay` loads a persisted run by ID, seeds a managed helper process, persists the replay run, and emits JSON
- `replay` loads the latest replayable run by session and ignores newer corrupted runs
- `inspect` validates selector usage
- file-like storage paths are preserved when resolving SQLite database paths

### File: `cmd/stagehand/inspect.go`

Purpose:

- implements the terminal-facing `stagehand inspect` workflow
- loads stored run records plus ordered interactions directly from SQLite
- renders nested interaction trees with optional body expansion

### Structs and their purposes

| Struct        | Purpose                                            |
| ------------- | -------------------------------------------------- |
| `inspectNode` | Tree node used to render parent/child interactions |

### Functions

| Function             | Signature                                                                                                                                                        | Purpose                                                               |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `runInspect`         | `func runInspect(args []string, stdout io.Writer, _ io.Writer) error`                                                                                            | Loads one run by ID or session and renders terminal inspection output |
| `loadInspectableRun` | `func loadInspectableRun(ctx context.Context, artifactStore store.ArtifactStore, runID string, session string) (store.RunRecord, []recorder.Interaction, error)` | Loads the selected run record and ordered interactions                |
| `renderInspect`      | `func renderInspect(w io.Writer, runRecord store.RunRecord, interactions []recorder.Interaction, showBodies bool) error`                                         | Renders the overall inspect view                                      |
| `buildInspectTree`   | `func buildInspectTree(interactions []recorder.Interaction) []*inspectNode`                                                                                      | Builds a nested tree from `parent_interaction_id` links               |
| `renderInspectNode`  | `func renderInspectNode(w io.Writer, node *inspectNode, depth int, showBodies bool, visited map[string]bool) error`                                              | Renders one interaction subtree                                       |
| `prettyJSON`         | `func prettyJSON(value any) string`                                                                                                                              | Pretty-prints request bodies and event payloads                       |
| `indentBlock`        | `func indentBlock(value string, prefix string) string`                                                                                                           | Applies indentation to multi-line body output                         |
| `formatLatency`      | `func formatLatency(latencyMS int64) string`                                                                                                                     | Renders interaction latency or `n/a`                                  |
| `formatFallback`     | `func formatFallback(tier recorder.FallbackTier) string`                                                                                                         | Renders fallback-tier labels or `none`                                |
| `emptyFallback`      | `func emptyFallback(value string, fallback string) string`                                                                                                       | Fills missing string fields in the renderer                           |

### Tests

File: `cmd/stagehand/inspect_test.go`

Covered behavior:

- `inspect` rejects missing or conflicting selectors
- `inspect` renders nested interactions with service, operation, latency, and fallback metadata
- `inspect --show-bodies` expands request bodies and event payloads
- `inspect --session` loads the latest stored run record even when the latest run is corrupted

### File: `cmd/stagehand/workflow.go`

Purpose:

- holds shared helpers for the real CLI record/replay workflows
- manages subprocess environment wiring, capture-bundle import/export, and local master-key handling
- normalizes imported interactions before recorder-side persistence

### Structs and their purposes

| Struct                | Purpose                                                       |
| --------------------- | ------------------------------------------------------------- |
| `interactionBundle`   | On-disk JSON envelope for capture or replay interaction lists |
| `recordResult`        | Machine-readable result emitted by `stagehand record`         |
| `replayCommandResult` | Machine-readable result emitted by `stagehand replay`         |

### Functions

| Function                        | Signature                                                                                                                                   | Purpose                                                                  |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `newRunRecordForMode`           | `func newRunRecordForMode(session string, cfg config.Config, mode recorder.RunMode) (store.RunRecord, error)`                               | Builds a mode-specific run record for CLI workflows                      |
| `newRecordingWriter`            | `func newRecordingWriter(artifactStore store.ArtifactStore, cfg config.Config, dbPath string) (*recording.Writer, error)`                   | Builds the scrub-before-persist writer used by CLI imports               |
| `loadOrCreateMasterKey`         | `func loadOrCreateMasterKey(storageDir string) ([]byte, error)`                                                                             | Loads a local master key or creates `stagehand.master.key` beside SQLite |
| `decodeMasterKey`               | `func decodeMasterKey(value string) ([]byte, error)`                                                                                        | Decodes raw, hex, or base64 master-key input                             |
| `runManagedCommand`             | `func runManagedCommand(ctx context.Context, commandArgs []string, extraEnv map[string]string, logOutput io.Writer) error`                  | Executes the managed child process with Stagehand env wiring             |
| `managedCommandEnvironment`     | `func managedCommandEnvironment(extraEnv map[string]string) []string`                                                                       | Merges process env, CLI overrides, and local `PYTHONPATH` wiring         |
| `loadInteractionBundle`         | `func loadInteractionBundle(path string) (interactionBundle, error)`                                                                        | Loads a capture/replay bundle from disk                                  |
| `writeInteractionBundle`        | `func writeInteractionBundle(path string, bundle interactionBundle) error`                                                                  | Writes a replay seed bundle atomically                                   |
| `normalizeImportedInteractions` | `func normalizeImportedInteractions(runID string, source []recorder.Interaction) []recorder.Interaction`                                    | Remaps imported interactions into the target run namespace               |
| `persistImportedInteractions`   | `func persistImportedInteractions(ctx context.Context, writer *recording.Writer, runID string, source []recorder.Interaction) (int, error)` | Persists normalized imported interactions                                |
| `emitJSON`                      | `func emitJSON(w io.Writer, value any) error`                                                                                               | Emits machine-readable JSON to stdout                                    |
| `mustGetwd`                     | `func mustGetwd() string`                                                                                                                   | Returns the working directory for local path helpers                     |

## Package `stagehand/internal/runtime/replay`

### File: `internal/runtime/replay/exact.go`

Purpose:

- validates a stored source run for exact replay
- summarizes the source run so the CLI can seed a managed replay subprocess and report stable machine-readable output

### Structs and their purposes

| Struct   | Purpose                                                                      |
| -------- | ---------------------------------------------------------------------------- |
| `Result` | Machine-readable summary of the exact replay source run used by the CLI flow |

### Exported functions and methods

| Function or Method | Signature                                      | Purpose                                                                   |
| ------------------ | ---------------------------------------------- | ------------------------------------------------------------------------- |
| `Exact`            | `func Exact(run recorder.Run) (Result, error)` | Validates replay eligibility and builds a source summary for exact replay |

### Tests

File: `internal/runtime/replay/exact_test.go`

Covered behavior:

- exact replay summarizes a replay-eligible run into stable machine-readable output
- non-replay-eligible runs are rejected with a concrete error

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
| `ScrubRule`            | One user-configurable structural scrub rule from config  |
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

| Function or Method        | Signature                                            | Purpose                                           |
| ------------------------- | ---------------------------------------------------- | ------------------------------------------------- |
| `DefaultConfig`           | `func DefaultConfig() Config`                        | Returns default runtime config                    |
| `DefaultTestConfig`       | `func DefaultTestConfig() TestConfig`                | Returns default test config                       |
| `Load`                    | `func Load(path string) (Config, error)`             | Loads and validates `stagehand.yml`               |
| `LoadTest`                | `func LoadTest(path string) (TestConfig, error)`     | Loads and validates `stagehand.test.yml`          |
| `(Config) Validate`       | `func (c Config) Validate() error`                   | Validates runtime config                          |
| `(TestConfig) Validate`   | `func (c TestConfig) Validate() error`               | Validates test config                             |
| `(ScrubConfig) Rules`     | `func (c ScrubConfig) Rules() ([]scrub.Rule, error)` | Converts and merges configured custom scrub rules |
| `(ValidationError) Error` | `func (e *ValidationError) Error() string`           | Returns aggregated validation message             |

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

File: `internal/config/fixture_test.go`

Covered behavior:

- valid runtime config fixtures load successfully
- strict YAML decoding rejects unknown fields
- schema version mismatch fixtures are rejected
- valid custom scrub rule fixtures load successfully
- conflicting custom scrub rules against built-ins are rejected
- invalid test replay combinations are rejected
- duplicate fallback tier fixtures are rejected

## Package `stagehand/internal/recorder`

### Files: `internal/recorder/doc.go`, `internal/recorder/schema.go`

Purpose:

- defines the canonical recording artifact schema for Stagehand runs
- centralizes validation of run, interaction, event, and scrub-report structure
- makes incomplete and corrupted run semantics explicit before storage and replay are implemented

### Schema/version constant

| Name                    | Type     | Meaning                                     |
| ----------------------- | -------- | ------------------------------------------- |
| `ArtifactSchemaVersion` | `string` | Current artifact schema version, `v1alpha1` |

### Enum-like type aliases

| Name                 | Underlying Type | Role                                                |
| -------------------- | --------------- | --------------------------------------------------- |
| `RunMode`            | `string`        | Artifact run mode                                   |
| `RunStatus`          | `string`        | Artifact integrity/completion state                 |
| `Protocol`           | `string`        | Captured protocol family                            |
| `FallbackTier`       | `string`        | Replay fallback tier used for an interaction        |
| `EventType`          | `string`        | Event kind inside an interaction                    |
| `IntegrityIssueCode` | `string`        | Machine-readable reason a run is incomplete/corrupt |

### Constant values

#### `RunMode`

- `RunModeRecord`
- `RunModeReplay`
- `RunModeHybrid`
- `RunModePassthrough`

#### `RunStatus`

- `RunStatusComplete`
- `RunStatusIncomplete`
- `RunStatusCorrupted`

#### `Protocol`

- `ProtocolHTTP`
- `ProtocolHTTPS`
- `ProtocolSSE`
- `ProtocolWebSocket`
- `ProtocolPostgres`

#### `FallbackTier`

- `FallbackTierExact`
- `FallbackTierNearestNeighbor`
- `FallbackTierStateSynthesis`
- `FallbackTierLLMSynthesis`

#### `EventType`

- `EventTypeRequestSent`
- `EventTypeResponseReceived`
- `EventTypeStreamChunk`
- `EventTypeStreamEnd`
- `EventTypeToolCallStart`
- `EventTypeToolCallEnd`
- `EventTypeError`
- `EventTypeTimeout`

#### `IntegrityIssueCode`

- `IntegrityIssueRecorderShutdown`
- `IntegrityIssueInterruptedWrite`
- `IntegrityIssueMissingEndState`
- `IntegrityIssueSchemaValidation`

### Structs and their purposes

| Struct            | Purpose                                       |
| ----------------- | --------------------------------------------- |
| `Run`             | Top-level persisted recording artifact        |
| `Interaction`     | One ordered external operation within a run   |
| `Request`         | Captured outbound request shape               |
| `Event`           | Ordered atomic event within an interaction    |
| `ExtractedEntity` | Semantic entity extracted from an interaction |
| `ScrubReport`     | Scrubbing provenance for one interaction      |
| `IntegrityIssue`  | Reason a run is incomplete or corrupted       |
| `ValidationError` | Aggregated artifact-validation error type     |

### Exported functions and methods

| Function or Method        | Signature                                  | Purpose                                             |
| ------------------------- | ------------------------------------------ | --------------------------------------------------- |
| `Load`                    | `func Load(path string) (Run, error)`      | Loads an artifact from disk with strict JSON decode |
| `Decode`                  | `func Decode(data []byte) (Run, error)`    | Decodes and validates artifact bytes                |
| `(Run) Validate`          | `func (r Run) Validate() error`            | Validates the full artifact structure               |
| `(Run) ReplayEligible`    | `func (r Run) ReplayEligible() bool`       | Returns true only for `complete` runs               |
| `(ValidationError) Error` | `func (e *ValidationError) Error() string` | Returns aggregated validation message               |

### Internal helpers

These are package-private implementation helpers, not public API:

- `(ValidationError).add`
- `(ValidationError).addNested`
- `(ValidationError).err`
- `(Interaction).validate`
- `(Request).validate`
- `(Event).validate`
- `(ExtractedEntity).validate`
- `(ScrubReport).validate`
- `(IntegrityIssue).validate`

### Tests

File: `internal/recorder/schema_test.go`

Covered behavior:

- complete artifacts validate successfully
- incomplete and corrupted runs require integrity issues
- complete runs cannot carry integrity issues
- interaction and event ordering violations are rejected
- scrub policy mismatch is rejected
- artifact JSON round-trips and still validates

File: `internal/recorder/fixture_test.go`

Covered behavior:

- valid artifact fixtures load successfully
- strict JSON decoding rejects unknown fields
- corrupted fixtures without integrity issues are rejected
- schema version mismatch fixtures are rejected

## Package `stagehand/internal/store`

### Files: `internal/store/doc.go`, `internal/store/store.go`

Purpose:

- defines the shared storage contract between the recorder/runtime layer and a concrete backend
- provides store-facing data models for runs, baselines, and scrub salts
- centralizes the not-found error contract for store implementations

### Variables

| Name          | Type    | Meaning                                 |
| ------------- | ------- | --------------------------------------- |
| `ErrNotFound` | `error` | Canonical missing-record error sentinel |

### Enum-like type aliases

| Name                 | Underlying Type | Role                                                    |
| -------------------- | --------------- | ------------------------------------------------------- |
| `RunLifecycleStatus` | `string`        | Store lifecycle state for in-progress and terminal runs |

### Constant values

#### `RunLifecycleStatus`

- `RunLifecycleStatusRunning`
- `RunLifecycleStatusComplete`
- `RunLifecycleStatusIncomplete`
- `RunLifecycleStatusCorrupted`

### Interfaces

| Name            | Purpose                                                     |
| --------------- | ----------------------------------------------------------- |
| `ArtifactStore` | Stable interface for local artifact persistence and queries |

Current `ArtifactStore` capabilities include:

- loading a specific run record
- loading the latest stored run record for a session
- loading the latest replayable complete run for a session
- loading ordered interactions for terminal or non-terminal runs

### Structs and their purposes

| Struct      | Purpose                                                         |
| ----------- | --------------------------------------------------------------- |
| `RunRecord` | Store-layer representation of top-level run metadata            |
| `ScrubSalt` | Session-scoped encrypted scrub salt record                      |
| `Baseline`  | Stored baseline metadata used for later diff/baseline selection |

### Exported functions and methods

| Function or Method           | Signature                                                                             | Purpose                                                                  |
| ---------------------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `RunRecordFromRun`           | `func RunRecordFromRun(run recorder.Run) RunRecord`                                   | Converts a recorder artifact into a store-layer record                   |
| `(RunRecord) ToRun`          | `func (r RunRecord) ToRun(interactions []recorder.Interaction) (recorder.Run, error)` | Rehydrates a terminal recorder run from stored metadata and interactions |
| `(RunRecord) ArtifactStatus` | `func (r RunRecord) ArtifactStatus() (recorder.RunStatus, error)`                     | Maps terminal lifecycle state to recorder artifact status                |
| `(RunRecord) Validate`       | `func (r RunRecord) Validate() error`                                                 | Validates the persisted top-level run record, including `running` state  |

### Tests

File: `internal/store/store_test.go`

Covered behavior:

- `running` lifecycle status validates successfully at the store layer
- `ToRun()` rejects `running` lifecycle state because it is not yet a terminal artifact
- terminal lifecycle states map correctly to recorder artifact status

## Package `stagehand/internal/store/contracttest`

### File: `internal/store/contracttest/contracttest.go`

Purpose:

- provides a reusable backend-agnostic artifact store contract suite
- defines the behavioral expectations future store backends must satisfy before they are considered compatible

### Types

| Name      | Kind           | Purpose                                              |
| --------- | -------------- | ---------------------------------------------------- |
| `Factory` | function alias | Opens a fresh `store.ArtifactStore` for one test run |

### Exported functions

| Function                | Signature                                                    | Purpose                                                         |
| ----------------------- | ------------------------------------------------------------ | --------------------------------------------------------------- |
| `RunArtifactStoreTests` | `func RunArtifactStoreTests(t *testing.T, newStore Factory)` | Runs the shared contract suite against a concrete store backend |

## Package `stagehand/internal/store/sqlite`

### Files: `internal/store/sqlite/doc.go`, `internal/store/sqlite/migrate.go`, `internal/store/sqlite/store.go`

Purpose:

- provides the local SQLite database bootstrap path for Stagehand
- applies embedded schema migrations in lexical order
- establishes the initial local schema for runs, interactions, events, assertions, baselines, and scrub salts
- implements the first concrete SQLite-backed artifact store

### Constants

| Name                | Type     | Meaning                                        |
| ------------------- | -------- | ---------------------------------------------- |
| `driverName`        | `string` | Database driver name for SQLite                |
| `migrationsTable`   | `string` | Metadata table recording applied migrations    |
| `defaultBusyTimout` | `int`    | SQLite busy-timeout pragma value in ms         |
| `sqliteTimeFormat`  | `string` | Canonical timestamp format used in stored rows |

### Structs and their purposes

| Struct      | Purpose                                        |
| ----------- | ---------------------------------------------- |
| `Migration` | In-memory representation of one SQL migration  |
| `Store`     | SQLite implementation of `store.ArtifactStore` |

### Exported functions

| Function         | Signature                                                                | Purpose                                                    |
| ---------------- | ------------------------------------------------------------------------ | ---------------------------------------------------------- |
| `Open`           | `func Open(path string) (*sql.DB, error)`                                | Opens a local SQLite database and applies base pragmas     |
| `OpenAndMigrate` | `func OpenAndMigrate(ctx context.Context, path string) (*sql.DB, error)` | Opens a DB and runs migrations                             |
| `Migrate`        | `func Migrate(ctx context.Context, db *sql.DB) error`                    | Applies unapplied embedded migrations                      |
| `NewStore`       | `func NewStore(db *sql.DB) (*Store, error)`                              | Wraps a migrated SQLite database in a store implementation |
| `OpenStore`      | `func OpenStore(ctx context.Context, path string) (*Store, error)`       | Opens, migrates, and returns the SQLite artifact store     |

### Exported methods on `Store`

| Method               | Signature                                                                                              | Purpose                                                              |
| -------------------- | ------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------- |
| `Close`              | `func (s *Store) Close() error`                                                                        | Closes the underlying SQLite handle                                  |
| `CreateRun`          | `func (s *Store) CreateRun(ctx context.Context, run store.RunRecord) error`                            | Persists a top-level run record                                      |
| `GetRunRecord`       | `func (s *Store) GetRunRecord(ctx context.Context, runID string) (store.RunRecord, error)`             | Loads top-level run metadata, including `running` runs               |
| `GetLatestRunRecord` | `func (s *Store) GetLatestRunRecord(ctx context.Context, sessionName string) (store.RunRecord, error)` | Loads the latest stored run record for a session across all statuses |
| `GetRun`             | `func (s *Store) GetRun(ctx context.Context, runID string) (recorder.Run, error)`                      | Loads a terminal run with all ordered interactions and events        |
| `GetLatestRun`       | `func (s *Store) GetLatestRun(ctx context.Context, sessionName string) (recorder.Run, error)`          | Loads the latest replayable complete run for a session               |
| `UpdateRun`          | `func (s *Store) UpdateRun(ctx context.Context, run store.RunRecord) error`                            | Updates run lifecycle and metadata fields                            |
| `DeleteRun`          | `func (s *Store) DeleteRun(ctx context.Context, runID string) error`                                   | Deletes one run and its dependent rows                               |
| `DeleteSession`      | `func (s *Store) DeleteSession(ctx context.Context, sessionName string) error`                         | Deletes all runs and scrub salt for a session                        |
| `WriteInteraction`   | `func (s *Store) WriteInteraction(ctx context.Context, interaction recorder.Interaction) error`        | Atomically writes one interaction and its events                     |
| `ListInteractions`   | `func (s *Store) ListInteractions(ctx context.Context, runID string) ([]recorder.Interaction, error)`  | Lists stored interactions for a run in sequence order                |
| `PutScrubSalt`       | `func (s *Store) PutScrubSalt(ctx context.Context, salt store.ScrubSalt) error`                        | Persists a session scrub salt                                        |
| `GetScrubSalt`       | `func (s *Store) GetScrubSalt(ctx context.Context, sessionName string) (store.ScrubSalt, error)`       | Loads a scrub salt or returns `store.ErrNotFound`                    |
| `PutBaseline`        | `func (s *Store) PutBaseline(ctx context.Context, baseline store.Baseline) error`                      | Persists a baseline record                                           |
| `GetBaseline`        | `func (s *Store) GetBaseline(ctx context.Context, baselineID string) (store.Baseline, error)`          | Loads a baseline by ID                                               |
| `GetLatestBaseline`  | `func (s *Store) GetLatestBaseline(ctx context.Context, sessionName string) (store.Baseline, error)`   | Returns the latest baseline for a session                            |

### Internal helpers

These are package-private implementation helpers, not public API:

- `ensureMigrationsTable`
- `loadMigrations`
- `loadAppliedVersions`
- `applyMigration`
- `getRunRecord`
- `listEventsForInteraction`
- `getBaselineByQuery`
- `ensureRunExists`
- `marshalJSON`
- `unmarshalJSONString`
- `formatTime`
- `formatOptionalTime`
- `parseStoredTime`
- `nullIfEmpty`
- `boolToInt`
- `nullableInt64`
- `nullableJSONString`
- `eventID`

### Embedded migration files

| File                                                                 | Purpose                                    |
| -------------------------------------------------------------------- | ------------------------------------------ |
| `internal/store/sqlite/migrations/0001_initial.sql`                  | Creates the initial local schema           |
| `internal/store/sqlite/migrations/0002_add_run_integrity_issues.sql` | Adds persisted run integrity issue storage |

### Tests

File: `internal/store/sqlite/migrate_test.go`

Covered behavior:

- a fresh SQLite database is created and migrated successfully
- required tables and indexes exist after migration
- rerunning migrations is idempotent
- opening with an empty path fails fast

File: `internal/store/sqlite/store_test.go`

Covered behavior:

- runs can be created, loaded, and updated
- running runs can be loaded as run records before they are terminal artifacts
- interactions and events round-trip through SQLite in sequence order
- invalid or partially failing interaction writes roll back atomically
- scrub salts persist and reload correctly
- baselines reject non-complete source runs
- baselines reject session/source-run mismatches
- baselines can be written, loaded, and selected by latest timestamp
- missing records map to `store.ErrNotFound`
- embedded migration count matches the expected migration set

File: `internal/store/sqlite/contract_test.go`

Covered behavior:

- SQLite satisfies the shared store contract for lifecycle transitions
- interaction ordering is stable regardless of insertion order
- incomplete and corrupted terminal runs hydrate correctly
- run and session deletion semantics match the shared contract

## Go Package Markers

The following files currently exist only to claim package names and document planned purpose:

| File                       | Package Purpose           | Current Implementation Depth              |
| -------------------------- | ------------------------- | ----------------------------------------- |
| `internal/analysis/doc.go` | analysis package          | marker only                               |
| `internal/runtime/doc.go`  | runtime package           | marker only                               |
| `internal/services/doc.go` | service simulator package | marker only                               |
| `internal/config/doc.go`   | config package            | marker only, real logic is in `config.go` |

The recorder and store packages are no longer marker-only because they now contain the artifact schema and shared store contract.

## Package `stagehand/internal/recording`

### Files: `internal/recording/writer.go`

Purpose:

- implements the safe recorder-side persisted write path
- enforces scrub-before-persist instead of relying on the SQLite layer to mutate payloads
- combines config-driven scrub rules, detector-driven free-text scrubbing, encrypted session salts, and store writes into one path

### Structs and their purposes

| Struct          | Purpose                                                                        |
| --------------- | ------------------------------------------------------------------------------ |
| `Writer`        | Coordinates scrub-before-persist interaction writes                            |
| `WriterOptions` | Construction input bundle for artifact store, salt manager, and scrub settings |

### Exported functions and methods

| Function or Method            | Signature                                                                                                                  | Purpose                                                                 |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `NewWriter`                   | `func NewWriter(opts WriterOptions) (*Writer, error)`                                                                      | Validates the persisted recording dependencies and builds a safe writer |
| `(Writer) PersistInteraction` | `func (w *Writer) PersistInteraction(ctx context.Context, interaction recorder.Interaction) (recorder.Interaction, error)` | Scrubs one interaction and persists only the scrubbed payload           |

### Tests

File: `internal/recording/writer_test.go`

Covered behavior:

- the safe writer rejects disabled scrub configuration
- persisted writes drop auth and cookie headers before SQLite persistence
- persisted writes detector-scrub standard secrets in headers, query params, bodies, and event payloads
- raw SQLite JSON rows no longer contain the original plaintext secrets after the safe writer runs

## Package `stagehand/internal/scrub`

### Files: `internal/scrub/doc.go`, `internal/scrub/pipeline.go`

Purpose:

- implements the structural scrub pipeline for pre-persistence interaction sanitization
- applies detector-driven free-text mutation for standard secrets and PII when no structural rule matches
- applies path-based rules to headers, query parameters, request bodies, and event payloads
- generates `ScrubReport` metadata describing which concrete paths were changed

### Enum-like type aliases

| Name     | Underlying Type | Role                           |
| -------- | --------------- | ------------------------------ |
| `Action` | `string`        | Structural scrub rule strategy |

### Constant values

#### `Action`

- `ActionDrop`
- `ActionMask`
- `ActionHash`
- `ActionPreserve`

### Structs and their purposes

| Struct            | Purpose                                                                              |
| ----------------- | ------------------------------------------------------------------------------------ |
| `Rule`            | One structural scrub rule with a path pattern and action                             |
| `Options`         | Pipeline construction inputs such as policy version, hash salt, and detector library |
| `Pipeline`        | Compiled structural scrub engine                                                     |
| `ValidationError` | Aggregated scrub-pipeline configuration error                                        |

### Exported functions and methods

| Function or Method            | Signature                                                                                             | Purpose                                                    |
| ----------------------------- | ----------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `DefaultRules`                | `func DefaultRules() []Rule`                                                                          | Returns the built-in auth and cookie header drop rules     |
| `MergeRules`                  | `func MergeRules(defaults []Rule, custom []Rule) ([]Rule, error)`                                     | Merges built-ins with validated custom rules               |
| `CloneRules`                  | `func CloneRules(rules []Rule) []Rule`                                                                | Returns a shallow copy of a rule slice                     |
| `MustMergeRules`              | `func MustMergeRules(defaults []Rule, custom []Rule) []Rule`                                          | Panics on invalid rules and returns merged output          |
| `NewPipeline`                 | `func NewPipeline(opts Options) (*Pipeline, error)`                                                   | Validates and compiles a scrub pipeline                    |
| `(Pipeline) ScrubInteraction` | `func (p *Pipeline) ScrubInteraction(interaction recorder.Interaction) (recorder.Interaction, error)` | Returns a scrubbed interaction plus generated scrub report |
| `(ValidationError) Error`     | `func (e *ValidationError) Error() string`                                                            | Returns aggregated pipeline-validation message             |

### Internal helpers

These are package-private implementation helpers, not public API:

- `scrubHeaders`
- `scrubQuery`
- `scrubValue`
- `scrubMap`
- `scrubSlice`
- `applyAction`
- `applyStringSliceAction`
- `hashValue`
- `maskValue`
- `hashString`
- `maskString`
- `stringValue`
- `deepCopy`
- `ruleForPath`
- `isValidAction`
- `compilePattern`
- `patternSpecificity`
- `joinObjectPath`
- `reportCollector`

### Tests

File: `internal/scrub/pipeline_test.go`

Covered behavior:

- default auth and cookie header rules drop sensitive request headers
- request query parameters can be hashed and written back into the URL
- request body fields can be masked through exact and wildcard paths
- event payload fields can be scrubbed through structural paths
- detector matches can mutate headers, query values, body strings, and event strings without explicit structural rules
- more specific `preserve` rules override broader wildcard rules
- hash output is deterministic within one pipeline configuration
- invalid hash-rule configuration is rejected
- detector-based scrubbing requires a hash salt
- masking a complex object is rejected with a concrete error

File: `internal/scrub/rules_test.go`

Covered behavior:

- custom rules are appended after built-in defaults
- custom rules cannot conflict with built-in exact patterns
- duplicate custom-rule patterns are rejected

## Package `stagehand/internal/scrub/detectors`

### Files: `internal/scrub/detectors/doc.go`, `internal/scrub/detectors/detectors.go`

Purpose:

- implements free-text and shape-independent detector primitives for scrub integration
- scans text for common secrets and PII without needing a structural JSON path
- returns stable ordered match metadata that scrub stages can consume

### Enum-like type aliases

| Name   | Underlying Type | Role               |
| ------ | --------------- | ------------------ |
| `Kind` | `string`        | Detector kind name |

### Constant values

#### `Kind`

- `KindEmail`
- `KindJWT`
- `KindPhone`
- `KindSSN`
- `KindCreditCard`
- `KindAPIKey`

### Interfaces

| Name       | Purpose                                 |
| ---------- | --------------------------------------- |
| `Detector` | Contract for one text detector strategy |

### Structs and their purposes

| Struct          | Purpose                                                        |
| --------------- | -------------------------------------------------------------- |
| `Enabled`       | Configuration mask for enabling a subset of standard detectors |
| `Match`         | One detector hit with kind, matched value, and byte offsets    |
| `Library`       | Ordered set of detectors that scans text and deduplicates hits |
| `regexDetector` | Internal regex-backed detector with optional semantic checks   |

### Exported functions and methods

| Function or Method        | Signature                                             | Purpose                                                 |
| ------------------------- | ----------------------------------------------------- | ------------------------------------------------------- |
| `DefaultLibrary`          | `func DefaultLibrary() *Library`                      | Builds the default email/JWT/phone/SSN/card/API-key set |
| `LibraryForEnabled`       | `func LibraryForEnabled(enabled Enabled) *Library`    | Builds a detector library from enabled-detector flags   |
| `NewLibrary`              | `func NewLibrary(detectors ...Detector) *Library`     | Builds a detector library from explicit detectors       |
| `(Library) Scan`          | `func (l *Library) Scan(text string) []Match`         | Scans text, deduplicates matches, and sorts by offset   |
| `(regexDetector) Kind`    | `func (d regexDetector) Kind() Kind`                  | Returns the detector kind                               |
| `(regexDetector) FindAll` | `func (d regexDetector) FindAll(text string) []Match` | Returns detector matches for one regex-backed strategy  |

### Internal helpers

These are package-private implementation helpers, not public API:

- `newRegexDetector`
- `validateEmail`
- `validateJWT`
- `validatePhone`
- `validateSSN`
- `validateCreditCard`
- `decodeBase64URL`
- `digitsOnly`
- `passesLuhn`
- `isPhoneTokenBoundary`
- `stringifyIndex`

### Tests

File: `internal/scrub/detectors/corpus_test.go`

Covered behavior:

- positive and negative corpus cases pass for email, JWT, phone, SSN, credit-card, and API-key detection
- mixed detector results are ordered by text offset
- overlapping API-key prefix regexes do not produce duplicate matches

## Package `stagehand/internal/scrub/session_salt`

### Files: `internal/scrub/session_salt/doc.go`, `internal/scrub/session_salt/manager.go`

Purpose:

- manages per-session scrub salt lifecycle in Go local mode
- encrypts salt material before persistence
- exposes deterministic replacement helpers used by hash-based scrubbing

### Constants

| Name            | Type  | Meaning                              |
| --------------- | ----- | ------------------------------------ |
| `SaltSize`      | `int` | Plaintext session salt size in bytes |
| `MasterKeySize` | `int` | Required AES-GCM master-key length   |

### Structs and their purposes

| Struct              | Purpose                                                               |
| ------------------- | --------------------------------------------------------------------- |
| `Manager`           | Coordinates session-salt generation, encrypted storage, and retrieval |
| `Material`          | In-memory plaintext session-salt material plus metadata               |
| `encryptedEnvelope` | Internal persisted AES-GCM envelope format                            |

### Exported functions and methods

| Function or Method      | Signature                                                                                  | Purpose                                                |
| ----------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------ |
| `NewManager`            | `func NewManager(artifactStore store.ArtifactStore, masterKey []byte) (*Manager, error)`   | Builds a session-salt manager over the artifact store  |
| `(Manager) Get`         | `func (m *Manager) Get(ctx context.Context, sessionName string) (Material, error)`         | Loads and decrypts an existing session salt            |
| `(Manager) GetOrCreate` | `func (m *Manager) GetOrCreate(ctx context.Context, sessionName string) (Material, error)` | Reuses or creates a session salt for one session       |
| `Replacement`           | `func Replacement(salt []byte, value string) (string, error)`                              | Produces deterministic session-scoped replacement text |

### Internal helpers

These are package-private implementation helpers, not public API:

- `(Manager).decrypt`
- `(Manager).encrypt`
- `(Manager).aead`
- `randomBytes`
- `generateSaltID`
- `associatedData`
- `looksLikeEmail`

### Tests

File: `internal/scrub/session_salt/manager_test.go`

Covered behavior:

- manager rejects invalid master-key length
- `GetOrCreate()` generates a new 32-byte salt and stores only encrypted bytes
- repeated `GetOrCreate()` for one session reuses the same stored salt
- deterministic replacement is stable within one session
- deterministic replacement differs across sessions

## Python Code

## Module `sdk/python/stagehand/__init__.py`

Purpose:

- current Python SDK package entrypoint
- re-exports the public bootstrap/runtime API surface

### Exports

| Export                     | Kind     | Purpose                                                            |
| -------------------------- | -------- | ------------------------------------------------------------------ |
| `__version__`              | string   | Python package version                                             |
| `ARTIFACT_VERSION`         | string   | Artifact version placeholder                                       |
| `StagehandRuntime`         | type     | Public runtime object returned by `init()`                         |
| `RuntimeMetadata`          | type     | Run/session metadata carried by the runtime                        |
| `AlreadyInitializedError`  | error    | Raised on double initialization                                    |
| `InvalidModeError`         | error    | Raised when `init()` receives an invalid mode                      |
| `NotInitializedError`      | error    | Raised when runtime state is requested too early                   |
| `ReplayMissError`          | error    | Raised when OpenAI replay mode has no exact seed                   |
| `init`                     | function | Initializes the Python SDK bootstrap                               |
| `init_from_env`            | function | Initializes the Python SDK from CLI-provided environment variables |
| `get_runtime`              | function | Returns the active runtime singleton                               |
| `is_initialized`           | function | Reports whether bootstrap has happened                             |
| `seed_replay_interactions` | function | Seeds the active runtime with exact replay data                    |

### Behavior

- `init(session, mode, config_path=None)` now creates a process-local runtime singleton
- bootstrap supports `record`, `replay`, and `passthrough`
- double initialization is rejected explicitly
- runtime metadata includes session, mode, generated `run_id`, optional config path, and version data
- bootstrap now also installs `httpx` interception and exposes captured interactions from the runtime
- replay mode can be seeded with captured OpenAI interactions for exact offline replay
- `init_from_env()` lets a CLI-managed subprocess pick up session, mode, config path, and replay/capture bundle wiring without custom argument parsing

## Module `sdk/python/stagehand/_runtime.py`

Purpose:

- implements the Python bootstrap singleton for Story D1
- validates bootstrap inputs
- resolves optional config paths
- owns the Python-side capture buffer used by the `httpx` interceptor
- exposes run/session metadata and captured interactions for recorder or CLI import hooks
- loads replay bundles and writes capture bundles when the CLI provides the relevant environment variables

### Type aliases

| Name            | Kind           | Purpose                                                    |
| --------------- | -------------- | ---------------------------------------------------------- |
| `StagehandMode` | `Literal[...]` | Allowed bootstrap modes: `record`, `replay`, `passthrough` |

### Constants

| Name                      | Meaning                                                        |
| ------------------------- | -------------------------------------------------------------- |
| `DEFAULT_CONFIG_FILENAME` | Default runtime config filename, `stagehand.yml`               |
| `BUNDLE_VERSION`          | JSON bundle envelope version used by CLI-managed record/replay |
| `ENV_SESSION`             | Environment variable carrying the Stagehand session            |
| `ENV_MODE`                | Environment variable carrying the Stagehand mode               |
| `ENV_CONFIG_PATH`         | Environment variable carrying the config path                  |
| `ENV_CAPTURE_OUTPUT`      | Environment variable naming the capture-bundle output file     |
| `ENV_REPLAY_INPUT`        | Environment variable naming the replay-seed input file         |
| `VALID_MODES`             | Canonical allowed bootstrap modes                              |

### Classes and dataclasses

| Name                      | Kind      | Purpose                                                              |
| ------------------------- | --------- | -------------------------------------------------------------------- |
| `StagehandError`          | exception | Base runtime bootstrap error                                         |
| `AlreadyInitializedError` | exception | Raised when bootstrap is attempted twice                             |
| `InvalidModeError`        | exception | Raised when mode is outside the supported set                        |
| `NotInitializedError`     | exception | Raised when runtime state is requested before init                   |
| `RuntimeMetadata`         | dataclass | Captures session, mode, run ID, config path, versions, and timestamp |
| `StagehandRuntime`        | dataclass | Public runtime wrapper around `RuntimeMetadata` and capture state    |

### Exported functions and methods

| Function or Method                            | Signature                                                                                                  | Purpose                                                            |
| --------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `init`                                        | `def init(session: str, mode: str, config_path: str \| Path \| None = None) -> StagehandRuntime`           | Initializes the runtime singleton                                  |
| `init_from_env`                               | `def init_from_env() -> StagehandRuntime`                                                                  | Initializes the runtime from CLI-provided environment variables    |
| `get_runtime`                                 | `def get_runtime() -> StagehandRuntime`                                                                    | Returns the active runtime                                         |
| `is_initialized`                              | `def is_initialized() -> bool`                                                                             | Reports whether bootstrap has already happened                     |
| `seed_replay_interactions`                    | `def seed_replay_interactions(interactions: Iterable[CapturedInteraction \| dict[str, Any]]) -> int`       | Seeds exact replay state for the active runtime                    |
| `RuntimeMetadata.as_recorder_dict`            | `def as_recorder_dict(self) -> dict[str, str \| None]`                                                     | Returns recorder-facing metadata in a serializable shape           |
| `StagehandRuntime.recorder_metadata`          | `def recorder_metadata(self) -> dict[str, str \| None]`                                                    | Convenience wrapper exposing the runtime metadata to recorder code |
| `StagehandRuntime.captured_interactions`      | `def captured_interactions(self) -> tuple[CapturedInteraction, ...]`                                       | Returns the in-memory normalized interaction buffer                |
| `StagehandRuntime.captured_interaction_dicts` | `def captured_interaction_dicts(self) -> list[dict[str, Any]]`                                             | Returns artifact-shaped interaction dictionaries                   |
| `StagehandRuntime.capture_bundle_dict`        | `def capture_bundle_dict(self) -> dict[str, Any]`                                                          | Returns the JSON bundle envelope used by CLI-managed record/replay |
| `StagehandRuntime.write_capture_bundle`       | `def write_capture_bundle(self, path: str \| Path) -> str`                                                 | Writes the captured interactions to a bundle file                  |
| `StagehandRuntime.seed_replay_interactions`   | `def seed_replay_interactions(self, interactions: Iterable[CapturedInteraction \| dict[str, Any]]) -> int` | Seeds OpenAI exact replay state for the current runtime            |

## Module `sdk/python/stagehand/_capture.py`

Purpose:

- defines the Python-side normalized interaction types used by Story D2
- converts `httpx` requests, responses, errors, and timeouts into the core interaction/event shape
- holds the thread-safe in-memory capture buffer until it is exported through the current CLI/import boundary or a future direct persistence path

### Constants

| Name                           | Meaning                                                           |
| ------------------------------ | ----------------------------------------------------------------- |
| `DEFAULT_SCRUB_POLICY_VERSION` | Placeholder scrub policy marker for pre-scrub in-memory captures  |
| `DEFAULT_SESSION_SALT_ID`      | Placeholder session salt marker until real scrub salt flows exist |

### Dataclasses and classes

| Name                  | Kind      | Purpose                                                            |
| --------------------- | --------- | ------------------------------------------------------------------ |
| `CapturedRequest`     | dataclass | Normalized request shape matching the artifact contract            |
| `CapturedEvent`       | dataclass | Normalized event entry for request, response, timeout, or error    |
| `CapturedScrubReport` | dataclass | Placeholder scrub provenance attached to in-memory captures        |
| `CapturedInteraction` | dataclass | Normalized interaction record aligned to the core artifact shape   |
| `CaptureBuffer`       | class     | Thread-safe in-memory buffer that assigns interaction ordering/IDs |

### Public behavior

- `CaptureBuffer.snapshot()` returns the captured interaction tuple for recorder or test inspection
- `CaptureBuffer.record_httpx_response(...)` appends a normalized response interaction
- `CaptureBuffer.record_httpx_failure(...)` appends a normalized timeout or error interaction
- `CaptureBuffer.record_openai_stream_response(...)` appends a streaming OpenAI interaction with ordered SSE events
- `CaptureBuffer.record_replay_interaction(...)` appends a cloned interaction for replay runs with fallback provenance
- the captured interactions can be exported through `StagehandRuntime.write_capture_bundle(...)`

## Module `sdk/python/stagehand/_httpx.py`

Purpose:

- installs and removes the Python SDK's monkey-patch for `httpx.Client.send`
- installs and removes the Python SDK's monkey-patch for `httpx.AsyncClient.send`
- routes outbound request activity into the in-memory `CaptureBuffer`

### Public functions

| Function                       | Signature                                                                                                              | Purpose                                      |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `install_httpx_interception`   | `def install_httpx_interception(*, mode: str, capture_buffer: CaptureBuffer, replay_store: OpenAIReplayStore) -> None` | Installs sync and async `httpx` interception |
| `uninstall_httpx_interception` | `def uninstall_httpx_interception() -> None`                                                                           | Restores original `httpx` methods            |

Behavior now covered:

- generic sync and async request interception
- OpenAI SSE capture by wrapping the response byte stream
- exact OpenAI replay returned directly from the `send` hook in replay mode
- CLI-seeded exact replay once `_runtime.py` has loaded a replay bundle into the runtime

## Module `sdk/python/stagehand/_openai.py`

Purpose:

- detects OpenAI requests that Stagehand can treat as provider-aware operations
- provides exact-match request fingerprinting for seeded replay
- raises a concrete replay-miss error when an OpenAI request is unseeded in replay mode

### Public classes and functions

| Name                | Kind      | Purpose                                                |
| ------------------- | --------- | ------------------------------------------------------ |
| `ReplayMissError`   | exception | Raised when replay mode has no exact seeded OpenAI hit |
| `OpenAIReplayStore` | class     | Stores queued exact-match replay interactions          |
| `is_openai_request` | function  | Identifies whether an `httpx.Request` targets OpenAI   |

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
- `init()` returns runtime metadata for valid bootstrap inputs
- default config autodiscovery works when `stagehand.yml` is present
- explicit config path resolution works
- double initialization is rejected
- invalid modes and empty sessions are rejected
- `get_runtime()` requires prior initialization

File: `sdk/python/tests/test_httpx_interception.py`

Covered behavior:

- sync `httpx` requests are captured and normalized into the interaction shape
- async `httpx` requests are captured through `AsyncClient.send`
- timeout exceptions are recorded as `timeout` events
- non-timeout request failures are recorded as `error` events
- runtime helpers can export artifact-shaped interaction dictionaries
- OpenAI SSE chunks are captured in order
- OpenAI tool-call boundaries are preserved as explicit events
- exact replay returns OpenAI responses through the same `httpx` streaming surface
- replay misses fail before the transport is touched
- the example onboarding agent can record once and replay offline

File: `sdk/python/tests/conftest.py`

Covered behavior:

- each Python test runs with a clean bootstrap singleton state

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
- re-exports the public TypeScript bootstrap/runtime API surface

### Type exports

| Name               | Kind        | Purpose                                                              |
| ------------------ | ----------- | -------------------------------------------------------------------- |
| `StagehandMode`    | union type  | Allowed mode values: `record`, `replay`, `passthrough`               |
| `InitOptions`      | object type | Input shape for `init()`                                             |
| `RuntimeMetadata`  | object type | Runtime metadata shape exposed on the `StagehandRuntime` object      |
| `RecorderMetadata` | object type | Recorder-facing serialized metadata returned by `recorderMetadata()` |

### Function exports

| Name            | Signature                                               | Purpose                                                         |
| --------------- | ------------------------------------------------------- | --------------------------------------------------------------- |
| `init`          | `function init(options: InitOptions): StagehandRuntime` | Initializes the TypeScript runtime singleton                    |
| `initFromEnv`   | `function initFromEnv(): StagehandRuntime`              | Initializes the runtime from CLI-provided environment variables |
| `getRuntime`    | `function getRuntime(): StagehandRuntime`               | Returns the active runtime singleton                            |
| `isInitialized` | `function isInitialized(): boolean`                     | Reports whether bootstrap has happened                          |

### Behavior

- `init()` now validates session and mode, resolves an optional config path, and creates a process-local runtime singleton
- `initFromEnv()` reads `STAGEHAND_SESSION`, `STAGEHAND_MODE`, and optional `STAGEHAND_CONFIG_PATH`
- the TypeScript bootstrap layer now matches Python behavior for initialization semantics before request interception work starts

## Module `sdk/typescript/src/runtime.ts`

Purpose:

- implements the TypeScript bootstrap singleton for Story G1
- validates bootstrap inputs
- resolves optional config paths
- exposes run/session metadata for later interception and recorder hooks

### Constants

| Name                      | Meaning                                             |
| ------------------------- | --------------------------------------------------- |
| `DEFAULT_CONFIG_FILENAME` | Default runtime config filename, `stagehand.yml`    |
| `ENV_SESSION`             | Environment variable carrying the Stagehand session |
| `ENV_MODE`                | Environment variable carrying the Stagehand mode    |
| `ENV_CONFIG_PATH`         | Environment variable carrying the config path       |
| `VALID_MODES`             | Canonical allowed bootstrap modes                   |

### Classes and types

| Name                      | Kind  | Purpose                                                              |
| ------------------------- | ----- | -------------------------------------------------------------------- |
| `StagehandError`          | error | Base TypeScript runtime bootstrap error                              |
| `AlreadyInitializedError` | error | Raised when bootstrap is attempted twice                             |
| `InvalidModeError`        | error | Raised when mode is outside the supported set                        |
| `NotInitializedError`     | error | Raised when runtime state is requested before init                   |
| `RuntimeMetadata`         | type  | Captures session, mode, run ID, config path, versions, and timestamp |
| `RecorderMetadata`        | type  | Serialized metadata passed to recorder-facing integrations later     |
| `StagehandRuntime`        | class | Public runtime wrapper around the bootstrap metadata                 |

### Exported functions and methods

| Function or Method                  | Signature                                               | Purpose                                                         |
| ----------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------- |
| `init`                              | `function init(options: InitOptions): StagehandRuntime` | Initializes the runtime singleton                               |
| `initFromEnv`                       | `function initFromEnv(): StagehandRuntime`              | Initializes the runtime from CLI-provided environment variables |
| `getRuntime`                        | `function getRuntime(): StagehandRuntime`               | Returns the active runtime                                      |
| `isInitialized`                     | `function isInitialized(): boolean`                     | Reports whether bootstrap has already happened                  |
| `_resetForTests`                    | `function _resetForTests(): void`                       | Clears the runtime singleton between tests                      |
| `StagehandRuntime.recorderMetadata` | `recorderMetadata(): RecorderMetadata`                  | Returns recorder-facing metadata in a serializable shape        |

### Behavior

- `stagehand.yml` autodiscovery works from the current working directory when present
- explicit config-path resolution fails fast when the file does not exist
- `runId` generation follows the same `run_<hex>` pattern used by the Python bootstrap
- request interception is still not implemented in TypeScript

## TypeScript tests

File: `sdk/typescript/src/index.test.ts`

Covered behavior:

- `init()` returns runtime metadata for valid bootstrap inputs
- default config autodiscovery works when `stagehand.yml` is present
- explicit config path resolution works
- `initFromEnv()` reads session, mode, and config path from environment variables
- double initialization is rejected
- invalid modes and empty sessions are rejected
- `getRuntime()` requires prior initialization

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

- runtime/session logic
- scrub engine logic
- diff engine
- assertion engine
- simulator implementations
- dashboard application
- persisted recorder/replay integration beyond seeded in-memory OpenAI exact replay

This distinction matters because the repo now has a stable shape, but only a subset of the planned system has actual behavior.
