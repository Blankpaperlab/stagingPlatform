# Behavior Contract Schema

Stagehand behavior contracts use YAML and validate before release review.

Current schema version: `v1alpha1`

The Go source of truth is `internal/analysis/contracts/schema.go`.

## File Shape

```yaml
schema_version: v1alpha1

agent:
  name: refund-agent

allowed_actions:
  - service: openai
    operation: POST /v1/chat/completions
    side_effect: read

  - tool: lookup_customer
    side_effect: read

restricted_actions:
  - service: stripe
    operation: POST /v1/refunds
    side_effect: financial
    max_amount: 5000
    requires_approval: true
    approval:
      source: human
      role: support_lead

forbidden_actions:
  - service: postgres
    operation: DELETE
    side_effect: destructive
    reason: destructive database write
```

## Top-Level Fields

| Field                | Required | Notes                                      |
| -------------------- | -------- | ------------------------------------------ |
| `schema_version`     | yes      | Must be `v1alpha1`                         |
| `agent.name`         | yes      | Human-readable agent name                  |
| `allowed_actions`    | no       | Approved actions that can pass review      |
| `restricted_actions` | no       | Approved actions with limits or approvals  |
| `forbidden_actions`  | no       | Actions that should always fail review     |
| `user_overrides`     | no       | Auditable manual changes to generated risk |

At least one action must exist across `allowed_actions`, `restricted_actions`, or `forbidden_actions`.

## Action Selectors

Each action is selected by either a service operation or a tool name.

```yaml
service: stripe
operation: POST /v1/refunds
```

```yaml
tool: lookup_customer
```

`tool` cannot be combined with `service` or `operation`.

## Side Effects

`side_effect` is required for actions and must be one of:

- `read`
- `write`
- `destructive`
- `financial`
- `external_message`
- `unknown`

## Fallback Tiers

`allowed_fallback_tiers` is optional. When present, values must be unique and in canonical order:

- `exact`
- `nearest_neighbor`
- `state_synthesis`
- `llm_synthesis`

## Approval Requirements

Actions can require approval:

```yaml
requires_approval: true
approval:
  source: human
  role: support_lead
  evidence_path: request.body.approval_id
```

`approval.source` must be one of `human`, `system`, or `external`.

## User Overrides

`user_overrides` records intentional edits to generated classifications or limits.

```yaml
user_overrides:
  - id: reviewed-refund-threshold
    match:
      service: stripe
      operation: POST /v1/refunds
    max_amount: 7500
    requires_approval: true
    reason: Temporary launch partner threshold approved for refund agent.
    approved_by: ops@example.com
    approved_at: 2026-06-07T12:00:00Z
```

Each override requires `id`, `match`, `reason`, and at least one override field. `approved_at`, when present, must be an RFC3339 timestamp.
