# Stagehand

Monorepo scaffold for the Stagehand local-first agent staging product.

Current status:

- Story A1 scaffold created
- Story A2 baseline CI automation added
- Story A3 packaging shells added
- Go toolchain installed locally and baseline Go build, vet, and test checks verified
- Python and TypeScript SDK packages are skeletons only
- Dashboard remains deferred in Plan A and is represented by a placeholder

Primary planning docs live under `docs/`.

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
