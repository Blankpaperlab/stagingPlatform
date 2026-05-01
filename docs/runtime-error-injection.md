# Runtime Error Injection

## Purpose

This document describes the current runtime error-injection support from Story `I3` plus the CLI-managed SDK workflow wiring from Story `Z3`.

The Go source of truth is:

- `internal/runtime/injection/engine.go`
- `internal/runtime/injection/library.go`
- `internal/runtime/injection/engine_test.go`
- `internal/services/stripe/simulator.go`
- `cmd/stagehand/workflow.go`
- `sdk/python/stagehand/_injection.py`
- `sdk/typescript/src/injection.ts`

## CLI-Managed SDK Workflows

`stagehand record` and `stagehand replay` accept `--error-injection <path>` for managed child processes:

```powershell
stagehand record --session refund-flow --error-injection error-injection.yml -- python agent.py
stagehand replay --session refund-flow --error-injection error-injection.yml -- python agent.py
```

The CLI file shape is:

```yaml
schema_version: v1alpha1
error_injection:
  rules:
    - name: Stripe refund fails on first attempt
      match:
        service: stripe
        operation: POST /v1/refunds
        nth_call: 1
      inject:
        status: 402
        body:
          error:
            type: card_error
            code: card_declined
            message: Your card was declined.
```

The CLI validates this YAML against the Go injection engine, resolves named library entries, writes a normalized JSON bundle, and passes its path to Python and TypeScript SDKs through `STAGEHAND_ERROR_INJECTION_INPUT`.

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

The Stripe simulator records applied injection provenance in memory and exposes it as run metadata through `ErrorInjectionMetadata`. Python and TypeScript SDK interception also attach applied provenance to the capture-bundle metadata. The CLI preserves capture-bundle metadata on the persisted run record, and SQLite persists top-level run metadata through `runs.metadata_json`. `stagehand inspect` renders run-level error-injection metadata when present.

## Current Limits

- The Stripe simulator core calls the engine before supported operations and returns injected Stripe-shaped errors without mutating session state.
- Python and TypeScript SDK adapters call the engine before live dispatch in record mode and before exact replay dispatch in replay mode.
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
- CLI-managed record and replay propagation through `--error-injection`
- Python and TypeScript SDK injection before live dispatch and before replay dispatch

## Demo

The executable failure-injection demo lives in `examples/failure-injection-demo`.

Run it from the repo root:

```powershell
go run ./examples/failure-injection-demo
```

The demo configures `stripe.card_declined` on the third `payment_intents.create` call, verifies the third call fails with a Stripe-shaped `402` response, verifies the fourth call succeeds as the third persisted PaymentIntent, and persists `error_injection.applied` provenance into run metadata.
