# Release Process

Use this checklist before cutting an external Stagehand release.

## Version Files

Keep these aligned:

- `VERSION`
- root `package.json`
- `internal/version/version.go`
- `sdk/python/pyproject.toml`
- `sdk/python/stagehand/_version.py`
- `sdk/typescript/package.json`
- `sdk/typescript/src/version.ts`

The Python package uses PEP 440 spelling, so `0.1.0-alpha.0` becomes
`0.1.0a0`.

## Build And Package

```bash
npm run package:release
```

This verifies version alignment, builds the Go CLI and daemon into
`dist/release`, builds the Python wheel/sdist, builds the TypeScript SDK, and
creates an npm tarball.

## Full Release Check

```bash
npm run release:check
```

This runs:

1. `npm run ci:all`
2. `npm run package:release`
3. `npm run verify:clean-onboarding`

## Artifacts

Expected local artifacts:

- `dist/release/stagehand-<version>-<platform>-<arch>/stagehand`
- `dist/release/stagehand-<version>-<platform>-<arch>/stagehandd`
- `sdk/python/dist/stagehand-<python-version>-py3-none-any.whl`
- `sdk/python/dist/stagehand-<python-version>.tar.gz`
- `dist/release/stagehand-sdk-<version>.tgz`

## Publish Notes

Before publishing:

- run the OpenAI verification agents in an environment allowed to call OpenAI
- run Stripe conformance with a Stripe test-mode key if the release claims
  Stripe simulator parity
- check that docs reference the same version being released
- fill in [RELEASE_NOTES_TEMPLATE.md](../RELEASE_NOTES_TEMPLATE.md)

Publishing commands are intentionally not automated yet. Keep the first public
release manual so package names, credentials, and registry permissions are
reviewed deliberately.
