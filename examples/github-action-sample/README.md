# Stagehand GitHub Action Sample

This directory is a copy-paste sample for repositories that want Stagehand replay diffs in pull requests.

The sample assumes:

- the repository builds `bin/stagehand`
- `stagehand.yml` points at the local Stagehand SQLite store
- `stagehand.test.yml` defines the default session and CI fail conditions
- at least one complete run has been promoted with `stagehand baseline promote`

## Workflow

```yaml
name: Stagehand

on:
  pull_request:

permissions:
  contents: read
  pull-requests: write
  issues: write

jobs:
  replay-diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go build -o bin/stagehand ./cmd/stagehand
      - uses: ./
        with:
          command: python examples/onboarding-agent/openai_demo_agent.py
          sessions: onboarding-flow
          baseline-source: latest
          assertions: stagehand.assertions.yml
```

The action uploads `.stagehand/action` and `.stagehand/runs` by default. The PR comment includes the uploaded artifact link plus local inspection commands.
