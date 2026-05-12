# Custom Tool Wrapper

Use Stagehand tools for local functions that your agent calls directly. A tool
call is recorded as a first-class interaction, replayed without calling the real
implementation, and included in inspect, diff, and assertions.

## Python

```python
import stagehand


@stagehand.tool(name="lookup_customer", side_effect="read", replay="recorded")
def lookup_customer(email: str) -> dict:
    return crm.lookup_customer(email)
```

In record mode, Stagehand calls the function, scrubs the arguments/result/error,
and stores the tool interaction.

In replay mode, Stagehand returns the recorded result or raises the recorded
error without calling `crm.lookup_customer`.

Async functions are supported:

```python
@stagehand.tool(name="async_lookup", side_effect="read", replay="recorded")
async def async_lookup(customer_id: str) -> dict:
    return await crm.lookup_customer(customer_id)
```

## TypeScript

```ts
const lookupCustomer = stagehand.tool(
  { name: 'lookup_customer', sideEffect: 'read', replay: 'recorded' },
  async ({ email }) => crm.lookupCustomer(email)
);
```

Sync and async functions are supported.

## What Gets Recorded

Each tool interaction records:

- tool name
- arguments
- result or error
- timing
- side-effect type
- replay policy
- call order
- parent/child links for nested tool calls
- scrub report

## Fail-Closed Replay

If replay reaches a tool call that was not recorded, Stagehand raises a replay
miss instead of inventing a result or calling the real function.

That is intentional. It catches agent drift.

## Assertions

Use tool interactions in assertions:

```yaml
schema_version: v1alpha1
assertions:
  - id: lookup-before-refund
    type: ordering
    before:
      tool: lookup_customer
    after:
      service: stripe
      operation: POST /v1/refunds
```

## Error Injection

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

## Example

See [examples/custom-tool-demo](../examples/custom-tool-demo/README.md) for a
private workflow with OpenAI, `lookup_customer`, internal billing, Stripe, and a
support-ticket API.
