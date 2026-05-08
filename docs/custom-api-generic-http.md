# Custom API Generic HTTP

Stagehand can capture, replay, diff, assert, and inject failures for your own HTTP APIs. A service mapping gives an internal host a stable Stagehand service name, so artifacts and reports say `internal-crm` or `internal-billing` instead of a raw hostname.

## Supported Behavior

Generic HTTP support is request/response oriented:

- records outbound HTTP calls made through the Python `httpx` SDK and TypeScript fetch/Undici interception
- labels mapped services by `services[*].name`
- labels operations as `METHOD /path`
- replays exact recorded responses offline
- ignores configured dynamic request fields before exact replay fingerprinting
- tolerates configured dynamic response fields during diffs
- supports tier-1 nearest-neighbor replay when `replay.allowed_tiers` includes `1`
- supports generic HTTP error injection for status overrides, malformed bodies, latency, and timeouts

This is enough when your internal API call is effectively a stable lookup or command whose recorded response is acceptable during replay.

## Exact Replay Is Enough

Exact generic HTTP replay is usually enough for:

- CRM lookups by email or customer ID
- entitlement or feature-flag reads
- pricing quote reads when request fields are stable
- billing preview endpoints that return deterministic payloads
- POST endpoints used as search or calculation calls

Configure dynamic request fields as ignored paths:

```yaml
services:
  - name: internal-crm
    type: api
    match:
      host: crm.internal.test
      path_prefix: /v1
    replay:
      mode: generic_http
      allowed_tiers: [0]
    ignore:
      request_paths:
        - headers.x-request-id
        - query.cursor
        - body.request_id
      response_paths:
        - body.generated_at
        - body.trace_id
```

With that mapping, a replay request that only changes `x-request-id`, `cursor`, or `request_id` still uses the recorded response.

## Nearest Neighbor

Use tier 1 only when the API is safe to replay from a similar recorded request:

```yaml
services:
  - name: internal-crm
    type: api
    match:
      host: crm.internal.test
      path_prefix: /v1
    replay:
      mode: generic_http
      allowed_tiers: [0, 1]
```

Tier 1 ignores common mutable fields such as request IDs, trace IDs, idempotency keys, pagination cursors/tokens, and timestamp-like values while scoring the nearest recorded request. Replay artifacts include `fallback_tier: nearest_neighbor` and `fallback_reason`.

## Error Injection

Generic HTTP injection can target internal services by stable service and operation labels:

```yaml
error_injection:
  - name: CRM timeout on third lookup
    service: internal-crm
    operation: POST /v1/customers/search
    nth_call: 3
    response:
      error: timeout
      latency_ms: 250

  - name: Billing API returns malformed 500
    service: internal-billing
    operation: POST /api/refunds
    probability: 1.0
    response:
      status: 500
      body: '{malformed-json'
```

Injected interactions include terminal-event `stagehand_injection` provenance, and run metadata includes `error_injection.applied`.

## Unsupported Stateful Semantics

Generic HTTP replay does not infer private API business state. It does not automatically:

- mutate an internal CRM or billing ledger
- synthesize new IDs
- enforce domain transitions
- emit webhooks or queued jobs
- derive later responses from earlier calls
- resolve concurrent writes or idempotency semantics

If the agent needs those semantics, exact generic HTTP is the wrong abstraction for that service.

## When A Simulator Is Needed

Use a prebuilt or user-defined simulator hook when:

- a create/update/delete call must affect later reads
- the API schedules callbacks, webhooks, or async jobs
- business rules matter, such as refund windows or account states
- the agent intentionally explores new request combinations not present in the recording
- replay must generate realistic IDs or timestamps instead of returning recorded ones

The V1 generic HTTP path deliberately keeps these semantics explicit. It gives stable replay, diffs, assertions, and failure injection for internal APIs without pretending to understand every company's state model.

## Demo

See [examples/custom-api-regression-demo](../examples/custom-api-regression-demo/README.md). It includes internal CRM and billing examples plus a CI-covered regression check.
