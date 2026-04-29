# Conformance Case Model

Stagehand conformance cases describe how to run a simulator-owned scenario against the real service and the simulator, then compare the two captured runs. The M1 schema is intentionally declarative: M2 owns execution, while M1 fixes the file shape and result contract.

Current schema version: `v1alpha1`

## File Shape

```yaml
schema_version: v1alpha1
cases:
  - id: stripe-payment-intent-happy-path
    service: stripe
    description: Create a customer, payment method, and succeeded payment intent.
    tags:
      - smoke
      - payment_intent
    inputs:
      scenario: payment_intent_happy_path
      steps:
        - id: create-customer
          operation: customers.create
          request:
            email: conformance@example.com
          expect:
            status_code: 200
        - id: create-payment-intent
          operation: payment_intents.create
          request:
            amount: 1200
            currency: usd
          expect:
            status: succeeded
    real_service:
      network: true
      test_mode: true
      cost_estimate: no charge in Stripe test mode
      credentials:
        - env: STRIPE_SECRET_KEY
          required: true
          purpose: Stripe test-mode API key.
    comparison:
      match:
        strategy: operation_sequence
      tolerated_diff_fields:
        - path: response.body.id
          reason: Real Stripe and simulator generate different object IDs.
          applies_to:
            - customers.create
            - payment_intents.create
        - path: response.body.created
          reason: Real service timestamps are wall-clock values.
```

## Case Inputs

`inputs.steps` is the portable scenario body. Each step must define:

- `id`: stable identifier used in result evidence
- `operation`: service operation name, such as `payment_intents.create`
- `request`: service-specific JSON-like request payload
- `expect`: optional service-specific expected response/status hints

The conformance runner should execute steps in file order. The case model does not prescribe a specific SDK, HTTP transport, or simulator adapter.

## Real-Service Requirements

`real_service` declares what a nightly or manual real-service run needs:

- `network`: whether the case must reach the public service
- `test_mode`: whether the service must run in a non-production/test account mode
- `credentials`: environment variables and purpose text required by the real runner
- `cost_estimate`: short operator-facing cost note

Required credentials must include both `env` and `purpose`. Missing required credentials should make the runner return `skipped`, not `failed`, because the simulator has not drifted.

## Matching and Tolerated Diffs

`comparison.match.strategy` defines how M2 aligns real and simulator interactions:

| Strategy               | Meaning                                                                  |
| ---------------------- | ------------------------------------------------------------------------ |
| `exact_sequence`       | Compare interactions by exact sequence position.                         |
| `operation_sequence`   | Compare the nth occurrence of each operation.                            |
| `interaction_identity` | Compare by explicit `comparison.match.keys` paths plus occurrence count. |

`comparison.tolerated_diff_fields` lists known non-semantic differences. Each entry must include:

- `path`: field path in the compared interaction/result object
- `reason`: why the difference is expected
- `applies_to`: optional list of operations where the tolerance applies

Typical tolerated fields are generated IDs, wall-clock timestamps, request IDs, and provider-specific metadata that the simulator should not reproduce exactly.

## Result Shape

The M2 runner emits one result per case:

```json
{
  "case_id": "stripe-payment-intent-happy-path",
  "service": "stripe",
  "status": "passed",
  "real_run": {
    "run_id": "run_real_123",
    "session_name": "conf_stripe_real",
    "mode": "record"
  },
  "simulator_run": {
    "run_id": "run_sim_123",
    "session_name": "conf_stripe_sim",
    "mode": "record"
  },
  "summary": {
    "matched_interactions": 2,
    "failing_diffs": 0,
    "tolerated_diffs": 3,
    "missing_interactions": 0,
    "extra_interactions": 0
  },
  "matches": [
    {
      "step_id": "create-customer",
      "real_interaction_id": "int_real_001",
      "simulator_interaction_id": "int_sim_001",
      "operation": "customers.create",
      "tolerated_fields": ["response.body.id", "response.body.created"]
    }
  ],
  "failures": []
}
```

Valid statuses are:

- `passed`: all non-tolerated comparisons matched
- `failed`: real and simulator behavior differed
- `skipped`: prerequisites, usually credentials, were unavailable
- `error`: the runner or harness failed before comparison

Failures should include a stable `code`, human-readable `message`, optional `step_id`, optional field `path`, and the real/simulator interaction IDs when available.

## Validation

The Go parser lives in `internal/analysis/conformance`. It rejects unknown fields, duplicate case IDs, empty case lists, missing services, empty step lists, missing step operations, invalid match strategies, missing credential env names, and tolerance entries without reasons.

## Runner Behavior

The first-pass runner also lives in `internal/analysis/conformance`.

- Real OpenAI cases support `chat.completions.create` and `responses.create` over the public API with `OPENAI_API_KEY`.
- Real Stripe cases support the simulator subset operations over Stripe test mode with `STRIPE_SECRET_KEY`.
- Stripe simulator cases execute against the in-process stateful simulator.
- OpenAI simulator cases use a deterministic local response path until a dedicated OpenAI simulator lands.
- Missing required real-service credentials produce `skipped` results.
- Structural comparison aligns by the configured match strategy and reports non-tolerated field mismatches as `field_mismatch` failures.

## CLI and Nightly Execution

Run conformance locally or in CI with:

```bash
stagehand conformance run \
  --cases conformance/smoke.yml \
  --config stagehand.yml \
  --output-dir .stagehand/conformance \
  --format github-markdown
```

The command writes:

- `.stagehand/conformance/conformance-results.json`
- `.stagehand/conformance/conformance-summary.md`

It exits non-zero when any case returns `failed` or `error`. Cases with missing required credentials return `skipped` and do not fail the command.

The nightly workflow lives at `.github/workflows/conformance-nightly.yml`. It runs once per day at 08:17 UTC and can also be started manually with `workflow_dispatch`. It uploads `.stagehand/conformance` as a workflow artifact and appends the markdown summary to the GitHub job summary.

## Cost and Frequency

The default smoke file is `conformance/smoke.yml`.

- OpenAI: one minimal chat-completion request per nightly run when `OPENAI_API_KEY` is configured.
- Stripe: one customer-create request in Stripe test mode when `STRIPE_SECRET_KEY` is configured; this should not create real charges.
- Missing secrets are expected in forks and local dry runs; those cases are reported as `skipped`.
- Keep new smoke cases small and deterministic. Broader conformance should run less frequently or behind manual dispatch until cost and flake rates are understood.
