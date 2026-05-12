# Getting Started

Stagehand turns one good agent run into a repeatable regression test. It records
LLM calls, provider API calls, internal HTTP calls, and custom tool calls, then
replays them offline and reports behavior changes.

## 1. Initialize

Run this in the repo that contains your agent:

```bash
stagehand init
```

This creates:

- `stagehand.yml`
- `.stagehand/runs`
- `.stagehand/reports`
- optional starter `assertions.yml` and `error-injection.yml`

Stagehand prints the command it thinks should run your agent, for example:

```bash
stagehand record-baseline -- python agent.py
```

## 2. Check Readiness

```bash
stagehand doctor
```

Fix any failed checks before recording. Warnings usually mean Stagehand found a
library it cannot intercept automatically, or an optional provider package is
missing.

## 3. Validate Your Entrypoint

For an existing app, use a small headless harness and validate it before live
recording:

```bash
stagehand preflight --task "Refund order 123" -- python stagehand_harness.py
```

The command must print one JSON object to stdout. Logs should go to stderr.

## 4. Record A Baseline

```bash
stagehand record-baseline -- python agent.py
```

This runs the agent in record mode, persists scrubbed interactions, promotes the
run as the latest baseline, and writes a first-run report under
`.stagehand/reports`.

## 5. Test Changes

```bash
stagehand test -- python agent.py
```

This replays the baseline offline, diffs the replay against the source run, runs
assertions when present, and writes JSON plus GitHub markdown reports.

## 6. Add CI

```bash
stagehand ci setup
```

Review the generated workflow, promote a baseline before enabling PR
enforcement, and add provider secrets to your CI environment.

## Examples

- onboarding: `examples/onboarding-agent`
- refund workflow: `examples/end-to-end-verification`
- support escalation: `examples/support-escalation-agent`
- custom APIs: `examples/custom-api-regression-demo`
- custom tools: `examples/custom-tool-demo`

## Next Docs

- [First-run onboarding](first-run-onboarding.md)
- [Existing agent harness](existing-agent-harness.md)
- [Baseline and CI](baseline-ci.md)
- [Limitations](limitations.md)
