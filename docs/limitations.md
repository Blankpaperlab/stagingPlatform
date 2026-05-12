# Limitations

This page describes what Stagehand V1 does and does not promise.

## Supported Capture Paths

Current supported paths include:

- Python OpenAI SDK and OpenAI-compatible HTTP hosts
- Python `httpx`
- Python Stripe SDK subset used by the refund examples
- Python `@stagehand.tool`
- TypeScript `fetch` and Undici
- TypeScript OpenAI-style SSE capture/replay
- TypeScript `stagehand.tool`
- mapped generic HTTP services

Run:

```bash
stagehand doctor
```

to detect common unsupported network libraries.

## Generic HTTP Is Not A Stateful Simulator

Generic HTTP replay returns recorded responses. It does not infer private
business state.

It does not automatically:

- mutate your CRM or billing database
- synthesize new IDs for unseen creates
- derive later reads from earlier writes
- emit webhooks or queued jobs
- enforce your domain state machine
- model concurrency or idempotency beyond recorded behavior

Use exact replay for deterministic lookups and command responses. Use a
simulator when durable state changes matter.

## Replay Is Intentionally Strict

Stagehand fails closed when replay cannot match a recorded interaction. This is
expected. It means the agent behavior changed.

If the mismatch is only noise, configure ignored fields. If the behavior is a
real API or workflow change, record and promote a new baseline.

## Nearest-Neighbor Fallback Is Opt-In

Tier-1 nearest-neighbor replay only applies where explicitly allowed. It is for
minor request variation, not for hiding behavior drift.

Artifacts record `fallback_tier` and `fallback_reason` so fallback use is
visible.

## Scrubbing Reduces Risk, Not Responsibility

Stagehand scrubs before persistence in the standard path, but run artifacts can
still be sensitive. Treat `.stagehand/runs` and uploaded reports as internal
engineering artifacts.

Review `inspect --show-bodies` output before sharing reports externally.

## Hosted Services Are Not Required

The current workflow is local-first. Recording, replay, diffing, assertions, and
CI reports can run without hosted Stagehand infrastructure.

## Live Verification Requires Credentials

Some verification paths are intentionally gated:

- OpenAI verification agents require `OPENAI_API_KEY`
- Stripe refund conformance requires `STRIPE_SECRET_KEY`

Without credentials, those checks skip or run only dry validation.

## Lower-Level Commands Remain Available

The guided commands are:

- `stagehand init`
- `stagehand record-baseline`
- `stagehand test`
- `stagehand ci setup`

The lower-level primitives remain available for advanced workflows:

- `record`
- `replay`
- `inspect`
- `diff`
- `assert`
- `baseline`
- `conformance`
