# Failure Injection Demo

This demo exercises the Story I3 Stripe failure-injection path.

It creates a local SQLite store, starts a Stripe simulator session, configures a deterministic `stripe.card_declined` error on the third `payment_intents.create` call, and runs four payment-intent attempts.

Expected behavior:

- attempts 1 and 2 succeed
- attempt 3 returns a Stripe-shaped `402 card_declined` injected error
- attempt 4 succeeds as `pi_000003`, proving the injected failure did not mutate state
- run metadata persists `error_injection.applied` provenance

Run it from the repo root:

```powershell
go run ./examples/failure-injection-demo
```

The same path is covered by `TestFailureInjectionDemoPasses`.
