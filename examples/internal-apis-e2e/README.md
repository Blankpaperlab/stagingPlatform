# Internal APIs E2E (Epic N)

A realistic end-to-end scenario showing how Stagehand handles agents that
orchestrate **multiple internal HTTP services** with dynamic per-call fields
(trace IDs, request IDs, idempotency keys, timestamps).

## The agent

[agent.py](agent.py) implements a **new-hire provisioning agent** that calls
three private services in sequence:

1. `internal-hr`: `POST /v1/employees/lookup` - fetch the employee profile
2. `internal-billing`: `POST /api/seats` - provision a workspace seat (idempotent)
3. `internal-notifier`: `POST /api/email` - queue a welcome email

Every call generates fresh values for headers like `x-request-id`,
`x-trace-id`, and `idempotency-key`, plus body fields like `request_id`,
`created_at`, and a `cursor` query parameter on the HR call.

## What Stagehand does

[stagehand.yml](stagehand.yml) maps each host to a stable service name
(`internal-hr`, `internal-billing`, `internal-notifier`) and lists the
per-service `ignore.request_paths` / `ignore.response_paths` that should be
treated as noise during exact replay and during diff comparison.

The flow:

- **Record mode** captures all three calls under their mapped service /
  operation labels (Epic N1 + N2).
- **Replay mode** re-runs the agent. Even though every dynamic field has a new
  value, exact replay still hits because the configured ignore paths are
  removed from the replay key (Epic N3).
- A change to a **non-ignored** field (the employee's email) fails closed with
  `ReplayMissError` - Stagehand will not let a regressed request quietly fall
  through to the live network.

The `internal-billing` mapping also opts into tier-1 nearest-neighbor
(`allowed_tiers: [0, 1]`) so future variant requests can pick the closest
neighbor when exact match misses (Epic N4).

## Running it

The full workflow runs as a pytest e2e:

```sh
cd sdk/python
python -m pytest tests/test_internal_apis_e2e.py -v
```

The test exercises:

| Phase                                      | What it proves                                |
| ------------------------------------------ | --------------------------------------------- |
| Record three internal calls                | Service mapping + operation inference (N1+N2) |
| Replay with brand-new dynamic field values | Per-service ignore paths suppress noise (N3)  |
| Replay with mutated non-ignored field      | Fail-closed on real regressions (N1)          |
| Per-service offline check                  | Each service replays without touching network |

## When this pattern fits

This pattern works when your internal APIs are **deterministic given the
business inputs** - same email + plan => same response. If the API mutates
durable state, depends on time, or branches off prior responses, you need a
prebuilt simulator or a custom simulator hook, not generic-HTTP exact replay.
