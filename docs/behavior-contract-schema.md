# Behavior Contract Schema

Stagehand behavior contracts use YAML and validate before release review.

Current schema version: `v1alpha1`

The Go source of truth is `internal/analysis/contracts/schema.go`.

Generate a starter contract from the latest promoted baseline:

```bash
stagehand contract generate --session refund-agent
```

By default this writes `stagehand.contract.yml` and refuses to overwrite an existing file unless `--force` is passed.

Generated contracts group actions by service or tool and include review comments above generated risk labels and high-risk release-gate suggestions. Small agents should produce a compact file that can be reviewed in one screen.

`stagehand test` and `stagehand review` load `stagehand.contract.yml` automatically when it is present. Use `--contract path` to require a specific contract file. Contract violations are emitted in the JSON report under `contract_violations` with exact interaction evidence.

Compare contract-level behavior between an approved base and a candidate run:

```bash
stagehand contract diff --session refund-agent --candidate-run-id run_123 --format github-markdown
```

The contract diff reports newly introduced actions, removed actions, side-effect changes, fallback tier changes, model changes, and captured prompt changes. Supported formats are `terminal`, `json`, and `github-markdown`.

## File Shape

```yaml
schema_version: v1alpha1

agent:
  name: refund-agent
  models:
    - gpt-5.4

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
| `agent.models`       | no       | Models observed in the approved baseline   |
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

Generated `side_effect` values are suggested labels. Review them before committing the contract, especially for `unknown`, `financial`, `destructive`, and `external_message`.

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

## Common Manual Edits

Move an action from `restricted_actions` to `allowed_actions` only when the generated label is too conservative and the action is safe without approval.

Change `side_effect: unknown` to a concrete label after reviewing what the service operation or tool does. Do not leave unknown-risk actions approved without a release gate.

Add `requires_approval: true` to financial, destructive, external-message, or sensitive write actions. Add `max_amount` when a financial action has an expected ceiling.

Add `forbidden_actions` for operations that should never run in production, such as destructive database writes or unapproved external messages.

Tighten `allowed_fallback_tiers` when an action must only be replayed or reviewed from exact evidence.
