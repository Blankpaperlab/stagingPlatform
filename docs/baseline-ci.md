# Baseline And CI Usage

Stagehand CI compares a replay run against a promoted baseline. The usual flow
is:

1. record one known-good run
2. promote it as the baseline
3. replay that baseline on every PR
4. fail CI on behavior diffs, assertion failures, or fallback regressions

## Record And Promote In One Command

For first-time setup:

```bash
stagehand record-baseline --session refund-flow -- python agent.py
```

This records the run and promotes it as the latest baseline for `refund-flow`.

Use a deterministic baseline ID when needed:

```bash
stagehand record-baseline \
  --session refund-flow \
  --baseline-id base_refund_v1 \
  -- python agent.py
```

## Lower-Level Promotion

If you already have a complete run:

```bash
stagehand baseline promote --run-id <run-id>
```

Show the latest baseline for a session:

```bash
stagehand baseline show --session refund-flow
```

## Run The Test Locally

```bash
stagehand test --session refund-flow -- python agent.py
```

`stagehand test` resolves the latest baseline, runs replay, diffs behavior, runs
assertions when configured, and writes reports under `.stagehand/reports`.

## Generate GitHub Actions Wiring

```bash
stagehand ci setup --session refund-flow --command "python agent.py"
```

Review the generated workflow before enforcing failures. The workflow includes
artifact upload and PR comment settings.

Dry-run without writing:

```bash
stagehand ci setup --dry-run --session refund-flow --command "python agent.py"
```

## Recommended PR Policy

Start with report-only mode while the team learns the signal. Then fail on:

- replay failure
- behavior diff
- assertion failure
- fallback regression

Keep baseline promotion intentional. A PR that changes desired behavior should
update code, update assertions if needed, record a new baseline, and make that
change visible in review.

## Artifacts

Important paths:

- `.stagehand/runs`: local SQLite store and run artifacts
- `.stagehand/reports`: JSON and markdown reports
- generated workflow artifacts from the GitHub Action

Use:

```bash
stagehand inspect --run-id <run-id> --show-bodies
stagehand diff --candidate-run-id <candidate> --baseline-id <baseline>
```

## GitHub Action Reference

See [github-action.md](github-action.md) for action inputs and PR comment
behavior.
