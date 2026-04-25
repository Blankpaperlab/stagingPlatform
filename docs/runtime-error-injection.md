# Runtime Error Injection

## Purpose

This document describes the current Story `I3` runtime error-injection slice.

The Go source of truth is:

- `internal/runtime/injection/engine.go`
- `internal/runtime/injection/library.go`
- `internal/runtime/injection/engine_test.go`
- `internal/services/stripe/simulator.go`

## Matcher Model

Rules match one runtime request at a time.

Supported match fields:

- `service`
- `operation`
- `nth_call`
- `any_call`
- `probability`

The engine keeps per-`service`/`operation` call counters. `nth_call: 3` means the third matching call for that service and operation is injected deterministically.

If `probability` is omitted, the rule is deterministic once the service, operation, and call-count matcher pass. If `probability` is present, it must be between `0` and `1`.

Rules are evaluated in list order. The first matching rule wins.

## Response Overrides

Rules can inject either:

- explicit `status` plus optional `body`
- a named `library` entry

Explicit overrides produce a `ResponseOverride` with:

- HTTP-style `status`
- JSON-like `body`

## Named Error Library

Current named entries:

- `stripe.card_declined`
- `stripe.insufficient_funds`
- `stripe.expired_card`
- `stripe.authentication_error`
- `stripe.rate_limit`
- `stripe.api_connection_error`

The Stripe entries return Stripe-shaped error response bodies with realistic status codes.

## Provenance

Each applied injection returns `Provenance`:

- `rule_index`
- `service`
- `operation`
- `call_number`
- `nth_call`
- `any_call`
- `probability`
- `library`
- `status`

`AppendProvenance` writes provenance under:

```json
{
  "error_injection": {
    "applied": []
  }
}
```

The Stripe simulator records applied injection provenance in memory and exposes it as run metadata through `ErrorInjectionMetadata`. The CLI preserves capture-bundle metadata on the persisted run record, and SQLite persists top-level run metadata through `runs.metadata_json`.

## Current Limits

- The Stripe simulator core calls the engine before supported operations and returns injected Stripe-shaped errors without mutating session state.
- Other future service adapters still need to call the engine before dispatching simulator operations.
- Probability uses an injectable random source for deterministic tests.
- Response overrides are JSON-like Go maps; the Stripe simulator maps them to typed Stripe errors, but no generic HTTP wire-format renderer is attached yet.

## Validation Coverage

Current tests cover:

- deterministic third-call failure injection
- probability `0` and `1` behavior
- explicit status/body response overrides
- named Stripe library entries
- Stripe simulator deterministic third-call failures
- Stripe simulator response overrides that do not mutate state
- unknown library rejection
- persisted run metadata provenance round-trip through SQLite

## Demo

The executable failure-injection demo lives in `examples/failure-injection-demo`.

Run it from the repo root:

```powershell
go run ./examples/failure-injection-demo
```

The demo configures `stripe.card_declined` on the third `payment_intents.create` call, verifies the third call fails with a Stripe-shaped `402` response, verifies the fourth call succeeds as the third persisted PaymentIntent, and persists `error_injection.applied` provenance into run metadata.
