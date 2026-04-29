# GitHub Action

Stagehand ships a repository-root action that replays one or more baseline-backed sessions, diffs the replay run, uploads the generated reports and run data, and posts a stable pull request comment.

## Sample Workflow

Use the action from a workflow after checking out the repository and building the CLI binary:

```yaml
name: Stagehand

on:
  pull_request:

permissions:
  contents: read
  pull-requests: write
  issues: write
  actions: read

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
          config: stagehand.yml
          test-config: stagehand.test.yml
          baseline-source: latest
          report-format: github-markdown
          fail-on: |
            behavior_diff
            assertion_failure
            fallback_regression
          assertions: stagehand.assertions.yml
          artifact-name: stagehand-run
```

For a published action, replace `uses: ./` with the tagged action reference.

## Outputs

- `report-path`: combined human-readable diff report.
- `summary-path`: machine-readable JSON summary.
- `comment-preview-path`: markdown body used for the PR comment.
- `artifact-paths`: newline-separated paths passed to `actions/upload-artifact`.
- `artifact-id` / `artifact-url`: upload metadata from `actions/upload-artifact@v4`.
- `failed`: `true` when configured fail conditions were triggered.

## PR Comment

The action posts or updates one comment per PR using a hidden Stagehand marker. The comment includes:

- pass/fail status
- artifact link when upload is enabled
- per-session base run, replay run, failing diff count, assertion failure count, and fail reasons
- local `stagehand inspect` and `stagehand diff` commands for each replay run
- the GitHub markdown diff report

If the workflow is not running on a pull request, comment posting is skipped and the same markdown is still written to `comment-preview-path`.
