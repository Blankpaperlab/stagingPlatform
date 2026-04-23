# Stagehand

Monorepo scaffold for the Stagehand local-first agent staging product.

Current status:

- Story A1 scaffold created
- Story A2 baseline CI automation added
- Story A3 packaging shells added
- Go toolchain installed locally and baseline Go build, vet, and test checks verified
- Story B1 config schemas implemented and validated
- Story B2 artifact schemas implemented and validated
- Story B3 schema fixtures and compatibility policy implemented
- Story C1 SQLite schema and migration runner implemented
- Story C2 SQLite store interface implemented
- Story C3 storage contract tests and store-level deletion semantics implemented
- Story D1 Python bootstrap implemented
- Story D2 Python `httpx` interception implemented
- Story D3 OpenAI-aware capture and exact replay implemented
- Story E1 structural scrub pipeline implemented
- Story E2 detector library implemented
- Story E3 session-scoped deterministic hashing implemented
- Story E4 user-configurable scrub rules implemented
- standard persisted recording path now scrubs before SQLite persistence and blocks plaintext standard secrets
- Story F1 CLI `record` implemented as a managed subprocess capture flow
- Story F2 CLI `replay` implemented as a managed subprocess exact-replay flow
- Story F3 CLI `inspect` implemented for terminal debugging of stored runs
- Python SDK now captures normalized in-memory HTTP interactions, can bootstrap from CLI-provided environment variables, and can replay seeded OpenAI chat-completion calls offline
- Story G1 TypeScript bootstrap runtime implemented
- Dashboard remains deferred in Plan A and is represented by a placeholder

Primary planning docs live under `docs/`.

Current schema references:

- config schema: `docs/config-schema.md`
- artifact schema: `docs/artifact-schema.md`
- validation and compatibility policy: `docs/schema-compatibility.md`
- structural scrub pipeline: `docs/scrub-structural-pipeline.md`
- detector library: `docs/scrub-detector-library.md`
- session hashing: `docs/scrub-session-hashing.md`
- local SQLite store and migrations: `docs/sqlite-local-store.md`

Baseline local checks:

1. Install root Node dependencies with `npm install`.
2. Install Python dev dependencies with `pip install -e "sdk/python[dev]"`.
3. Ensure `go` is installed for Go lint/test commands.
4. Run `npm run ci:all` from the repo root.

Current local CLI workflow:

1. Run `stagehand record --session <name> -- <command> [args...]` to launch a Stagehand-aware subprocess, collect its exported capture bundle, scrub it, and persist the run to SQLite.
2. Run `stagehand replay (--run-id <id> | --session <name>) -- <command> [args...]` to seed a Stagehand-aware subprocess from a stored run and persist the replay run result.
3. Run `stagehand inspect (--run-id <id> | --session <name>) [--show-bodies]` to inspect ordered interactions, nested calls, and failed-run integrity issues from the terminal.
4. In Python child processes, call `stagehand.init_from_env()` or `stagehand.init(...)` so the SDK picks up the CLI-provided session/mode wiring.

Build commands:

1. Build the CLI with `npm run build:go:cli`.
2. Build the daemon with `npm run build:go:daemon`.
3. Build the Python SDK package with `npm run build:python`.
4. Build the TypeScript SDK package with `npm run build:typescript`.
5. Build all available artifacts with `npm run build:all`.

Version placeholders:

- root artifact version: `VERSION`
- Go runtime version constant: `internal/version/version.go`
- Python SDK package and artifact versions: `sdk/python/stagehand/_version.py`
- TypeScript SDK and artifact versions: `sdk/typescript/src/version.ts`
