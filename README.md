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
- Minimal Python OpenAI verification agents live in `examples/verification-agents/` and exercise the core record, scrub, store, replay, and inspect loop
- Python and TypeScript SDKs currently support `record`, `replay`, and `passthrough` modes; hybrid remains a future runtime concept, not a shipped SDK mode
- Story G1 TypeScript bootstrap runtime implemented
- Story G2 TypeScript request interception implemented for built-in `fetch` and `undici`
- TypeScript SDK now emits OpenAI-style streaming chunk and tool-call boundary events during SSE capture
- TypeScript SDK replay now supports exact seeded replay for non-streaming and supported SSE interactions and fails closed on replay misses instead of hitting live APIs
- Python and TypeScript OpenAI-aware capture can classify additional compatible hosts through `STAGEHAND_OPENAI_HOSTS`
- Story H1 runtime session lifecycle implemented for create, snapshot, restore, fork, destroy, and session isolation
- Story H2 runtime event queue implemented for persisted scheduled events, session sim clocks, `AdvanceTime`, and push/pull delivery modes
- Story H3 runtime fallback tiers 0-2 implemented for exact matching, nearest-neighbor matching, state-synthesis hooks, and persisted fallback tier tracking
- Story H4 runtime resilience implemented for terminal run classification, import preflight validation, and actionable integrity issues
- Story I1 Stripe simulator subset implemented for customers, payment methods, and payment intents with session-backed state
- Story I2 Stripe business rules and side effects implemented for state consistency, realistic transition errors, webhook scheduling, and customer identity extraction
- Story I3 runtime error injection implemented for service/operation/call/probability matching, response overrides, named Stripe errors, and persisted provenance metadata
- Stripe failure-injection demo lives in `examples/failure-injection-demo` and is covered by CI
- Dashboard remains deferred in Plan A and is represented by a placeholder

Primary planning docs live under `docs/`.

Current schema references:

- config schema: `docs/config-schema.md`
- artifact schema: `docs/artifact-schema.md`
- validation and compatibility policy: `docs/schema-compatibility.md`
- structural scrub pipeline: `docs/scrub-structural-pipeline.md`
- detector library: `docs/scrub-detector-library.md`
- session hashing: `docs/scrub-session-hashing.md`
- runtime session lifecycle: `docs/runtime-session-lifecycle.md`
- runtime event queue and sim time: `docs/runtime-event-queue.md`
- runtime fallback tiers: `docs/runtime-fallback-tiers.md`
- runtime resilience behavior: `docs/runtime-resilience.md`
- runtime error injection: `docs/runtime-error-injection.md`
- Stripe simulator subset: `docs/stripe-simulator-subset.md`
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

Minimal OpenAI verification agents:

1. Install the pinned example dependency with `python -m pip install -r examples/verification-agents/requirements.txt`.
2. Set `OPENAI_API_KEY`.
3. Run `python examples/verification-agents/verify_openai_agents.py`.
4. The verifier runs the simple, tool-calling, and sensitive-data agents in order, then checks SQLite persistence, scrub reports, offline replay, and inspect output.

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
