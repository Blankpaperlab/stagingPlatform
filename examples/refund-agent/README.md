# Refund Agent Example

The first-party refund example lives in
[`examples/end-to-end-verification`](../end-to-end-verification). It is the
launch-grade refund workflow and regression suite for Stagehand.

The flow demonstrates:

- model-driven refund approval
- Stripe customer and payment lookup
- Stripe refund creation
- sensitive-data scrubbing
- offline replay
- diff and assertion failures
- deterministic HTTP error injection
- CI workflow wiring

Run the dry verification path:

```bash
examples/end-to-end-verification/verify.sh
```

Run the live-gated path with OpenAI and Stripe test credentials:

```bash
OPENAI_API_KEY=sk-... STRIPE_SECRET_KEY=sk_test_... examples/end-to-end-verification/verify.sh --live
```

Core capability demonstrated: a realistic agent workflow can be recorded once,
replayed offline, compared against a changed behavior, asserted for business
safety, and exercised under injected provider failures.
