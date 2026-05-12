# Support Escalation Agent Example

This example shows a private support workflow that combines a local Stagehand
tool with an internal HTTP API.

The flow:

1. wraps `lookup_customer` with `@stagehand.tool`
2. decides whether the case should be escalated
3. creates a ticket through `support-ticket-api`

Core capability demonstrated: Stagehand preserves ordering across custom tool
calls and generic HTTP calls, then replays both offline from the same recorded
interactions.

Files:

- `agent.py`: runnable Python support escalation agent
- `stagehand.yml`: maps `support.internal.acme.test` to `support-ticket-api`
  and ignores request IDs / generated response metadata
- `assertions.yml`: starter ordering and forbidden-operation assertions

Automated coverage:

```bash
cd sdk/python
python -m pytest tests/test_support_escalation_example.py -v
```
