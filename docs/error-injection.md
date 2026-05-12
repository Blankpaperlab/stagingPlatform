# Error Injection

Use error injection to test how an agent behaves when a provider, internal API,
or custom tool fails.

## Run With Injection

Create `error-injection.yml`, then run:

```bash
stagehand test --error-injection error-injection.yml -- python agent.py
```

You can also use lower-level commands:

```bash
stagehand record --session refund-flow --error-injection error-injection.yml -- python agent.py
stagehand replay --session refund-flow --error-injection error-injection.yml -- python agent.py
```

## HTTP Timeout

```yaml
schema_version: v1alpha1
error_injection:
  enabled: true
  rules:
    - name: CRM timeout on first lookup
      match:
        service: internal-crm
        operation: POST /v1/customers/search
        nth_call: 1
      inject:
        error: timeout
```

## HTTP Status And Body

```yaml
schema_version: v1alpha1
error_injection:
  enabled: true
  rules:
    - name: Billing returns 500
      match:
        service: internal-billing
        operation: POST /api/refunds
        nth_call: 1
      inject:
        status: 500
        body:
          error: internal_server_error
```

## Tool Error

```yaml
schema_version: v1alpha1
error_injection:
  enabled: true
  rules:
    - name: lookup failure on second call
      match:
        tool: lookup_customer
        nth_call: 2
      inject:
        error: not_found
        body:
          message: customer missing
          error_class: CustomerNotFoundError
```

## Matching

Rules can match by:

- `service`
- `operation`
- `tool`
- `nth_call`
- `any_call`
- `probability`

Rules are evaluated in order. The first matching rule wins.

Use `nth_call` for deterministic recovery tests. Use `probability` sparingly;
deterministic tests are usually better for CI.

## Evidence

Injected interactions are stored like normal interactions and include
`stagehand_injection` provenance. `stagehand inspect`, `stagehand diff`, and
assertion evidence can point at the concrete injected call.

## Examples

- generic HTTP: `examples/failure-injection-demo`
- custom tool failure: `examples/custom-tool-demo`
- refund workflow injection: `examples/end-to-end-verification/error-injection.yml`

## Reference

See [runtime-error-injection.md](runtime-error-injection.md) for the full rule
model and engine behavior.
