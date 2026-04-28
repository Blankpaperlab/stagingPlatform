# Assertion Schema

Stagehand assertion files use YAML and validate before execution.

Current schema version: `v1alpha1`

## File Shape

```yaml
schema_version: v1alpha1
assertions:
  - id: exactly-three-payment-intents
    type: count
    match:
      service: stripe
      operation: payment_intents.create
    expect:
      count:
        equals: 3
```

## Assertion Types

J1 defines and validates six assertion types. J2 executes the five core types. J3 adds shallow `cross-service` execution.

| Type                   | Purpose                                           | Required fields                         |
| ---------------------- | ------------------------------------------------- | --------------------------------------- |
| `count`                | Assert how many interactions match a selector     | `match`, `expect.count`                 |
| `ordering`             | Assert one matched interaction appears before one | `before`, `after`                       |
| `payload-field`        | Assert a selected request/response/event field    | `match`, `expect.path`, one comparator  |
| `forbidden-operation`  | Assert an operation did not occur                 | `match.service`, `match.operation`      |
| `fallback-prohibition` | Assert selected interactions avoided tiers        | `match`, `expect.disallowed_tiers`      |
| `cross-service`        | Assert two service entity references are linked   | `left`, `right`, `relationship: equals` |

## Selectors

`match`, `before`, and `after` use the same selector shape:

```yaml
match:
  service: stripe
  operation: payment_intents.create
  interaction_id: int_123
  event_type: response_received
  fallback_tier: exact
```

At least one selector field is required when a selector is present.

## Expectations

`count` supports either exact count or a range:

```yaml
expect:
  count:
    equals: 3
```

```yaml
expect:
  count:
    min: 1
    max: 3
```

`payload-field` supports exactly one comparator:

```yaml
expect:
  path: response.body.error.code
  equals: card_declined
```

```yaml
expect:
  path: request.body.customer
  exists: true
```

`fallback-prohibition` supports disallowed tiers:

```yaml
expect:
  disallowed_tiers:
    - llm_synthesis
```

## Cross-Service References

`cross-service` uses entity references instead of `match`:

```yaml
left:
  service: stripe
  operation: customers.create
  path: response.body.id
right:
  service: stripe
  operation: payment_intents.create
  path: request.body.customer
relationship: equals
```

The evaluator extracts values from interactions matching each entity reference and passes when at least one left/right pair is equal. If a referenced path resolves to an object or array, scalar leaf values are compared so simple one-hop and shallow nested links are supported.

## Validation Behavior

The parser rejects:

- unknown YAML fields
- missing or unsupported `schema_version`
- empty assertion lists
- duplicate assertion IDs
- unsupported assertion types
- invalid field combinations for each assertion type

Validation errors include assertion indexes and field paths, for example:

```text
invalid assertion file: assertions[0].expect.count cannot combine equals with min or max
```

## Execution Results

The evaluator runs assertions against a `recorder.Run` and returns one result per assertion:

- `passed`
- `failed`
- `unsupported`

The executable assertion types are:

- `count`
- `ordering`
- `payload-field`
- `forbidden-operation`
- `fallback-prohibition`
- `cross-service`

Results include assertion ID, type, status, message, and evidence. Evidence includes matched interaction IDs, concrete violating interactions, observed counts, actual field values, disallowed fallback tiers, and linked entity pairs where relevant.
