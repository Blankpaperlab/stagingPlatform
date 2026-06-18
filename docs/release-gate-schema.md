# Release Gate Schema

`stagehand.gates.yml` defines release gates that block or constrain agent behavior during review. The schema is strict: unknown YAML fields are rejected so typos fail early.

`stagehand test` and `stagehand review` load `stagehand.gates.yml` automatically when it is present. Use `--gates path` to require a specific gate file. Release gates are translated into the existing assertion evaluator; `assertions.yml` remains available as the advanced assertion API.

`stagehand init` writes a short starter `stagehand.gates.yml` with gates for destructive database actions, external message approvals, high-value financial approvals, and unknown-risk review.

```yaml
schema_version: v1alpha1

release_gates:
  - name: Refunds over $50 require approval
    block_if:
      service: stripe
      operation: POST /v1/refunds
      amount_gt: 5000
      approval_missing: true

  - name: Agent cannot send external customer messages
    block_if:
      side_effect: external_message
      destination_type: external

  - name: Customer lookup must happen before billing action
    require_order:
      before:
        tool: lookup_customer
      after:
        service: billing-api
```

## Root Fields

- `schema_version`: required. Must be `v1alpha1`.
- `release_gates`: required. A non-empty list of gate definitions.

Each release gate must include `name` and exactly one gate clause.

## Selectors

Gate clauses match actions with selector fields:

- `service`: service name, such as `stripe` or `billing-api`.
- `operation`: operation name or HTTP route, such as `POST /v1/refunds`. If set, `service` must also be set.
- `tool`: local/custom tool name. Cannot be combined with `service` or `operation`.
- `side_effect`: one of `read`, `write`, `destructive`, `financial`, `external_message`, or `unknown`.
- `destination_type`: destination label captured by review, such as `external`.

## Gate Clauses

`block_if` blocks when all configured condition fields match. It supports selector fields plus `amount_gt`, `approval_missing`, `channel`, `domain`, `new_action`, and `unknown_risk`.

`require_order` requires one action to appear before another:

```yaml
require_order:
  before:
    tool: lookup_customer
  after:
    service: billing-api
```

`require_approval` requires approval before a matching action. It supports selector fields and optional `amount_gt`.

`max_amount` caps a matching action amount:

```yaml
max_amount:
  service: stripe
  operation: POST /v1/refunds
  amount: 5000
```

`allowed_channels` and `allowed_domains` restrict matched external actions to explicit values:

```yaml
allowed_channels:
  side_effect: external_message
  values: [email, sms]

allowed_domains:
  side_effect: external_message
  values: [example.com, support.example.com]
```

`forbid_new_action` blocks newly introduced matching actions.

`forbid_unknown_risk` blocks actions that still have unknown risk. Use `{}` to apply it globally or add selector fields to scope it.

## Review Output

Gate results appear separately from advanced assertions in terminal, JSON, and markdown reports. User-facing output uses gate names, while JSON also includes the backing internal assertion ID for debugging. Failed gates include concise evidence with matched interaction IDs, sequence numbers, services, and operations.
