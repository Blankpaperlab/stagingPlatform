# Custom API Regression Demo

This demo shows Stagehand concepts for private HTTP APIs that do not have prebuilt provider profiles.

It models two internal services:

- `internal-crm`: `POST /v1/customers/search`
- `internal-billing`: `POST /api/refunds`

The demo records stable service and operation labels, replays CRM and billing responses with exact generic HTTP matching, ignores common dynamic fields such as cursors and idempotency keys, and then runs a regression comparison that catches a billing behavior change.

Run it:

```sh
go run ./examples/custom-api-regression-demo
```

The demo persists two inspectable runs into the local Stagehand SQLite store:

- `run_custom_api_base`
- `run_custom_api_candidate`

After running the demo, inspect and diff the persisted artifacts with:

```sh
go run ./cmd/stagehand inspect --run-id run_custom_api_base --show-bodies
go run ./cmd/stagehand inspect --session custom-api-regression-demo --show-bodies
go run ./cmd/stagehand diff --candidate-run-id run_custom_api_candidate --base-run-id run_custom_api_base
```

CI covers it through:

```sh
go test ./examples/custom-api-regression-demo
```

Use this pattern when your agent calls internal APIs with deterministic request and response behavior. If the API mutates durable business state, emits webhooks, or derives later responses from earlier calls, use a prebuilt simulator or a user-defined simulator hook when that extension point exists.
