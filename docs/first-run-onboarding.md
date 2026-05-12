# First-Run Onboarding

Use this path when you are adding Stagehand to a repo for the first time.

## The Short Path

```bash
stagehand init
stagehand doctor
stagehand record-baseline -- python agent.py
stagehand test -- python agent.py
```

Replace `python agent.py` with the command that runs one deterministic agent
scenario.

## What `stagehand init` Writes

`stagehand init` creates a minimal local scaffold:

- `stagehand.yml`: record/replay/scrub config
- `.stagehand/runs`: local SQLite run storage
- `.stagehand/reports`: generated markdown and JSON reports
- `.stagehand/generated`: future generated files
- `assertions.yml`: commented starter assertions
- `error-injection.yml`: commented starter failure rules

It does not overwrite an existing config unless you pass `--force`.

## Choosing The First Scenario

Pick one workflow that matters and can run repeatedly:

- support refund request
- user onboarding
- support escalation
- internal CRM lookup plus billing action
- one model call plus one custom tool

Keep the first scenario narrow. A good first baseline has enough interactions to
prove value, but not so many that every app change causes noise.

## Harness Contract

The managed command should:

- initialize the Stagehand SDK or call `stagehand.init_from_env()`
- run one scenario
- print a JSON result to stdout when used as a harness
- write logs to stderr
- avoid background work that continues after process exit

Validate this before recording:

```bash
stagehand preflight --task "Escalate billing issue" -- python stagehand_harness.py
```

## Recording

```bash
stagehand record-baseline --session refund-flow -- python agent.py
```

This records interactions, promotes the run to a baseline, and prints:

- run ID
- baseline ID
- storage path
- capture summary
- next `stagehand test` command
- first-run report path

## Replaying

```bash
stagehand test --session refund-flow -- python agent.py
```

Stagehand resolves the latest baseline for the session, replays recorded
interactions, compares behavior, evaluates assertions, and writes reports.

## When The First Run Captures Nothing

Check:

- the command uses a supported SDK or HTTP path
- the process initializes Stagehand
- the agent actually makes a model, HTTP, provider, or tool call
- unsupported network clients are not bypassing interception

Run:

```bash
stagehand doctor
```

Then inspect your harness against [existing-agent-harness.md](existing-agent-harness.md).
