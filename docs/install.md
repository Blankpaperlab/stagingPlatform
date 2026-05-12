# Installation

Stagehand currently ships as three installable pieces:

- Go CLI binaries: `stagehand` and `stagehandd`
- Python SDK package: `stagehand`
- TypeScript SDK package: `@stagehand/sdk`

Use the CLI for record/replay/diff/assert workflows. Install the SDK that
matches the agent runtime you are testing.

## From A Release Archive

Download the archive for your platform, unpack it, and put `stagehand` on your
`PATH`.

```bash
stagehand --help
```

The daemon binary, `stagehandd`, is packaged beside the CLI for future hosted or
service-mode workflows.

## From Source

```bash
git clone https://github.com/Blankpaperlab/stagingPlatform.git
cd stagingPlatform
npm install
npm run build:go:all
```

Then use:

```bash
./bin/stagehand --help
```

On Windows:

```powershell
.\bin\stagehand.exe --help
```

## Python SDK

For development from this repo:

```bash
python -m pip install -e "sdk/python[dev]"
```

From a built wheel:

```bash
python -m pip install sdk/python/dist/stagehand-0.1.0a0-py3-none-any.whl
```

In an agent harness:

```python
import stagehand

stagehand.init_from_env()
```

## TypeScript SDK

For development from this repo:

```bash
npm install
npm run -w @stagehand/sdk build
```

From a packed tarball:

```bash
npm install ./dist/release/stagehand-sdk-0.1.0-alpha.0.tgz
```

In an agent harness:

```ts
import { initFromEnv } from '@stagehand/sdk';

initFromEnv();
```

## Verify A Clean Onboarding Path

After building the CLI, run:

```bash
npm run verify:clean-onboarding
```

This creates a temporary project, runs `stagehand init`, `doctor`, `preflight`,
`record-baseline`, and `test` against a tiny Python tool-based agent.
