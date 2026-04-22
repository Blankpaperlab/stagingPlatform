# Config Schema

## Purpose

This document defines the current V1 config contract for:

- `stagehand.yml`
- `stagehand.test.yml`

The Go source of truth is `internal/config/config.go`.

Validation and compatibility policy for this schema is defined in `docs/schema-compatibility.md`.

## File Roles

### `stagehand.yml`

Runtime and capture defaults:

- record behavior
- replay behavior
- scrub policy
- fallback policy
- auth behavior

### `stagehand.test.yml`

Test-runner behavior:

- baseline selection
- replay restrictions
- disallowed fallback tiers
- error injection
- CI reporting and fail conditions

## `stagehand.yml`

### Top-level fields

| Field            | Required | Default    | Notes                                 |
| ---------------- | -------- | ---------- | ------------------------------------- |
| `schema_version` | no       | `v1alpha1` | Must match the current schema version |
| `record`         | no       | see below  | Record-time defaults                  |
| `replay`         | no       | see below  | Replay-time defaults                  |
| `scrub`          | no       | see below  | Scrub policy                          |
| `fallback`       | no       | see below  | Fallback policy                       |
| `auth`           | no       | see below  | Auth resolver defaults                |

### `record`

| Field                           | Required | Default                      | Validation                        |
| ------------------------------- | -------- | ---------------------------- | --------------------------------- |
| `default_mode`                  | no       | `record`                     | `record`, `replay`, `passthrough` |
| `storage_path`                  | no       | `.stagehand/runs`            | must be non-empty                 |
| `capture.max_body_bytes`        | no       | `1048576`                    | must be greater than `0`          |
| `capture.include_headers`       | no       | `["content-type", "accept"]` | cannot contain empty entries      |
| `capture.redact_before_persist` | no       | `true`                       | must remain `true` in V1          |

### `replay`

| Field                | Required | Default | Validation               |
| -------------------- | -------- | ------- | ------------------------ |
| `mode`               | no       | `exact` | `exact`, `prompt_replay` |
| `clock_mode`         | no       | `wall`  | V1 allows `wall` only    |
| `allow_live_network` | no       | `false` | boolean                  |

### `scrub`

| Field                | Required | Default | Validation                                                                            |
| -------------------- | -------- | ------- | ------------------------------------------------------------------------------------- |
| `enabled`            | no       | `true`  | must remain `true` in V1                                                              |
| `policy_version`     | no       | `v1`    | must be non-empty                                                                     |
| `custom_rules`       | no       | `[]`    | validated rule list, merged after built-in defaults                                   |
| `custom_rules_files` | no       | `[]`    | reserved in V1, must remain empty                                                     |
| `detectors.*`        | no       | `true`  | `email`, `phone`, `ssn`, `credit_card`, `jwt`, and `api_key` must remain `true` in V1 |

#### `scrub.custom_rules[*]`

| Field     | Required | Validation                           |
| --------- | -------- | ------------------------------------ |
| `name`    | yes      | non-empty, unique among custom rules |
| `pattern` | yes      | non-empty, unique among custom rules |
| `action`  | yes      | `drop`, `mask`, `hash`, `preserve`   |

Rule-merge behavior:

- custom rules are appended after built-in defaults
- custom rules cannot reuse the exact pattern of a built-in rule
- custom rules cannot duplicate another custom rule's exact `name` or `pattern`
- scrub cannot be disabled in the standard V1 persisted-recording path
- the built-in detector set cannot be disabled in V1 because persisted recording must not store standard secrets in plaintext

Example:

```yaml
scrub:
  policy_version: v1
  custom_rules:
    - name: customer-email-mask
      pattern: request.body.customer.email
      action: mask
    - name: support-id-preserve
      pattern: request.body.ticket.support_id
      action: preserve
    - name: query-token-hash
      pattern: request.query.token
      action: hash
```

### `fallback`

| Field                        | Required | Default                                            | Validation                                    |
| ---------------------------- | -------- | -------------------------------------------------- | --------------------------------------------- |
| `allowed_tiers`              | no       | `["exact", "nearest_neighbor", "state_synthesis"]` | must be valid, unique, and in canonical order |
| `llm_synthesis.enabled`      | no       | `false`                                            | boolean                                       |
| `llm_synthesis.provider_env` | no       | `""`                                               | required if llm synthesis is enabled          |
| `llm_synthesis.model`        | no       | `""`                                               | required if llm synthesis is enabled          |

Canonical tier order:

1. `exact`
2. `nearest_neighbor`
3. `state_synthesis`
4. `llm_synthesis`

### `auth`

| Field           | Required | Default      | Validation                                               |
| --------------- | -------- | ------------ | -------------------------------------------------------- |
| `default_mode`  | no       | `permissive` | `permissive`, `strict-recorded`, `strict-match`          |
| `service_modes` | no       | `{}`         | service names must be non-empty and use valid auth modes |

## `stagehand.test.yml`

### Top-level fields

| Field             | Required | Default    | Notes                                 |
| ----------------- | -------- | ---------- | ------------------------------------- |
| `schema_version`  | no       | `v1alpha1` | Must match the current schema version |
| `session`         | yes      | none       | Required for test runs                |
| `baseline`        | no       | see below  | Baseline selection                    |
| `replay`          | no       | see below  | Test replay controls                  |
| `fallback`        | no       | see below  | CI-level fallback policy              |
| `error_injection` | no       | see below  | Failure simulation                    |
| `ci`              | no       | see below  | PR and CI behavior                    |

### `baseline`

| Field              | Required | Default | Validation        |
| ------------------ | -------- | ------- | ----------------- |
| `branch`           | no       | `main`  | must be non-empty |
| `require_existing` | no       | `true`  | boolean           |

### `replay`

| Field                 | Required | Default | Validation                                      |
| --------------------- | -------- | ------- | ----------------------------------------------- |
| `mode`                | no       | `exact` | `exact`, `prompt_replay`                        |
| `forbid_live_network` | no       | `true`  | cannot be `true` when `mode` is `prompt_replay` |

### `fallback`

| Field              | Required | Default             | Validation                      |
| ------------------ | -------- | ------------------- | ------------------------------- |
| `disallowed_tiers` | no       | `["llm_synthesis"]` | valid tiers only, no duplicates |

### `error_injection`

| Field     | Required | Default | Validation                   |
| --------- | -------- | ------- | ---------------------------- |
| `enabled` | no       | `false` | boolean                      |
| `rules`   | no       | `[]`    | each rule validates as below |

Per-rule match fields:

| Field               | Required | Validation                             |
| ------------------- | -------- | -------------------------------------- |
| `match.service`     | yes      | non-empty                              |
| `match.operation`   | yes      | non-empty                              |
| `match.nth_call`    | no       | must be `>= 0`                         |
| `match.any_call`    | no       | cannot be combined with `nth_call > 0` |
| `match.probability` | no       | must be between `0` and `1`            |

Per-rule inject fields:

| Field            | Required    | Validation                                 |
| ---------------- | ----------- | ------------------------------------------ |
| `inject.library` | conditional | use this for named simulator errors        |
| `inject.status`  | conditional | required when `inject.library` is not used |
| `inject.body`    | conditional | optional alongside `inject.status`         |

Rules cannot mix `inject.library` with explicit `status/body`.

### `ci`

| Field               | Required | Default                                  | Validation                                                                  |
| ------------------- | -------- | ---------------------------------------- | --------------------------------------------------------------------------- |
| `fail_on`           | no       | `["behavior_diff", "assertion_failure"]` | at least one of `behavior_diff`, `assertion_failure`, `fallback_regression` |
| `report_format`     | no       | `github-markdown`                        | `terminal`, `json`, `github-markdown`                                       |
| `artifact_name`     | no       | `stagehand-run`                          | must be non-empty                                                           |
| `post_pr_comment`   | no       | `true`                                   | boolean                                                                     |
| `max_fallback_tier` | no       | `nearest_neighbor`                       | must be a valid fallback tier                                               |

## Environment-Varying Values

These fields are expected to vary by environment and should not be treated as globally fixed constants:

- `record.storage_path`
- `scrub.custom_rules`
- `fallback.llm_synthesis.provider_env`
- `fallback.llm_synthesis.model`
- `auth.service_modes`
- `session`
- `baseline.branch`
- `error_injection.rules`
- `ci.artifact_name`
- `ci.post_pr_comment`

V1 does not expand environment variables automatically inside YAML. Use your shell, CI system, or templating step to generate environment-specific values if needed.

## Validation Errors

The config package returns aggregated validation errors. A single invalid file can report multiple problems at once, for example:

- invalid enum value
- missing required field
- contradictory replay policy
- invalid fallback tier ordering
- malformed error injection rule

## Sample Files

Committed sample files:

- `stagehand.yml`
- `stagehand.test.yml`

These files are intended to be the human-readable reference configuration for the current schema.

Fixture-backed validation coverage for this schema lives under `internal/config/testdata/`.
