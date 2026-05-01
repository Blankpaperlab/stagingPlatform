# End-to-End Refund Verification

This example is the Epic Z launch-demo path. It exercises the refund workflow through:

- Python OpenAI and Stripe SDK capture
- scrubbed SQLite persistence
- exact offline replay
- baseline promotion and diff rendering
- assertion evaluation with concrete evidence
- deterministic error injection
- Stripe real-vs-simulator conformance

## Files

- `agent.py`: refund processing agent.
- `agent_modified.py`: same agent with a changed OpenAI classification prompt for diff verification.
- `assertions.yml`: shipped `v1alpha1` assertion schema with five assertion types.
- `error-injection.yml`: first refund call fails deterministically.
- `error-injection-nth3.yml`: third refund call fails deterministically for retry verification.
- `verify.sh`: local verification script.
- `stagehand-pr-workflow.yml`: copy-paste GitHub Actions workflow using the local unpublished action.

## Dry Run

Dry mode validates local schemas and verifies the Stripe refund conformance case is marked `skipped` when `STRIPE_SECRET_KEY` is absent.

```bash
bash examples/end-to-end-verification/verify.sh
```

The script builds the current local CLI when Go is available. If you run it from WSL against a Windows checkout, use a Linux-built `stagehand` binary or run from a Windows-native shell with Bash available; a Windows `bin/stagehand` cannot read WSL `/mnt/c/...` paths.

## Live Run

Install live dependencies and set test credentials:

```bash
python -m pip install openai stripe
export OPENAI_API_KEY=sk-...
export STRIPE_SECRET_KEY=sk_test_...
bash examples/end-to-end-verification/verify.sh --live
```

The script writes artifacts under `.stagehand/e2e`, including run JSON, agent outputs, diffs, assertion reports, and conformance reports.

Live mode uses a unique fixture email per script run to keep Stripe test-mode search results from accumulating into future baselines. It also verifies replay-miss fail-closed behavior, session-salt isolation, schema/scrub-policy pins, scrubbed API key/email/phone/JWT/card/idempotency-key persistence, `inspect --show-bodies`, and persisted error-injection provenance.

## Shipped CLI Shapes

Assertions run against run IDs:

```bash
stagehand assert --run-id <run-id> --assertions assertions.yml --format json
```

Diffs use a candidate run plus one base selector:

```bash
stagehand diff --candidate-run-id <candidate-run-id> --baseline-id <baseline-id>
```

Conformance uses plural `--cases`:

```bash
stagehand conformance run --cases conformance/stripe-refund-smoke.yml
```

Offline replay verification does not use macOS-only `pfctl`. The script unsets live provider credentials for replay and relies on Stagehand's fail-closed replay behavior.

## GitHub Actions

`stagehand-pr-workflow.yml` is the Z6 sample workflow. Copy it to `.github/workflows/stagehand-refund-verification.yml` in a repository that has already promoted a `refund-flow-baseline` run. The workflow intentionally uses the local unpublished action:

```yaml
uses: ./
```

The action invocations use the shipped input names: `command`, `sessions`, `baseline-source`, `assertions`, `fail-on`, `artifact-name`, and `comment-id`. The workflow runs the action twice with distinct `comment-id` values so two Stagehand comments can coexist on one PR.

Required permissions:

- `contents: read`
- `pull-requests: write`
- `issues: write`
- `actions: read`

Required secrets for live conformance:

- `STRIPE_SECRET_KEY`: Stripe test-mode secret key. If it is absent, the refund conformance case is marked `skipped`.

Replay/diff does not require live OpenAI or Stripe credentials because it replays the promoted baseline. The workflow uploads `.stagehand/runs`, `.stagehand/e2e`, `.stagehand/conformance`, and the action report directory as artifacts. It fails on configured behavior diffs, assertion failures, fallback regressions, and conformance drift.

The sample workflow pins `STAGEHAND_CUSTOMER_EMAIL=refund-conformance@example.com`; promote the baseline with the same environment, or edit that value to match the baseline you promoted.
