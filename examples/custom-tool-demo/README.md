# Custom Tool Demo

This demo exercises Epic Y custom tool support in one private business workflow.

The workflow contains:

- OpenAI planning call
- custom `lookup_customer` tool
- internal billing API refund
- Stripe refund
- internal support-ticket API

It then demonstrates the full Stagehand loop:

- record the workflow once
- replay the recorded workflow offline
- diff a changed support-ticket behavior
- fail an assertion when billing happens before `lookup_customer`
- inject a typed `lookup_customer` tool failure and verify recovery through a support ticket

Run it:

```bash
go run ./examples/custom-tool-demo
```

The demo persists inspectable runs into `.stagehand/runs/stagehand.db`:

- `run_custom_tool_record`
- `run_custom_tool_replay`
- `run_custom_tool_candidate`
- `run_custom_tool_unsafe_order`
- `run_custom_tool_injected`

Useful follow-up commands:

```bash
go run ./cmd/stagehand inspect --session custom-tool-demo --show-bodies
go run ./cmd/stagehand diff --candidate-run-id run_custom_tool_candidate --base-run-id run_custom_tool_record
go run ./cmd/stagehand assert --run-id run_custom_tool_unsafe_order --assertions examples/custom-tool-demo/assertions.yml
```

CI coverage:

```bash
go test ./examples/custom-tool-demo
```
