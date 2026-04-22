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
- Python SDK now captures normalized in-memory HTTP interactions and can replay seeded OpenAI chat-completion calls offline
- TypeScript SDK package is still scaffold-only
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
