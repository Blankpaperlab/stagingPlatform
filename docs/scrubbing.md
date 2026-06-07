# Scrubbing

Stagehand scrubs captured interactions before durable persistence. The standard
recording path is designed so stored artifacts do not contain common secrets or
PII in plaintext.

## What Gets Scrubbed

The default scrub policy detects common sensitive values in headers, query
parameters, request bodies, response bodies, events, tool arguments, tool
results, and tool errors.

Common detector categories include:

- emails
- phone numbers
- SSNs
- credit cards
- JWTs
- API keys
- passwords and secret-like fields
- authorization, proxy authorization, API-key, idempotency-key, cookie, and
  set-cookie headers

Scrubbed interactions include a `scrub_report` that records which paths were
changed.

## Deterministic Replacements

Stagehand uses session-scoped deterministic replacements. That means the same
customer email inside one session is replaced consistently, so replay and
cross-service assertions can still match related values without storing the raw
value.

Example:

```text
jane.doe@example.com -> user_<token>@scrub.local
```

## Custom Rules

Add project-specific rules in `stagehand.yml` when your payload contains fields
that the built-in detectors cannot infer safely.

```yaml
scrub:
  enabled: true
  policy_version: v1
  custom_rules:
    - path: request.body.customer.internal_note
      replace_with: '[scrubbed]'
    - path: response.body.metadata.private_segment
      replace_with: '[scrubbed]'
```

Use custom rules for internal IDs, tenant names, proprietary labels, or any
domain-specific payload field that should not persist.

## Verifying Scrubbing

Inspect a run:

```bash
stagehand inspect --run-id <run-id> --show-bodies
```

Look for:

- scrubbed values in request and response bodies
- `scrub_report.redacted_paths`
- no plaintext provider keys or sensitive user data

The OpenAI verification agents also check SQLite persistence directly:

```bash
python examples/verification-agents/verify_openai_agents.py
```

That live verifier requires `OPENAI_API_KEY`.

## Practical Guidance

- Keep `redact_before_persist: true`.
- Treat `.stagehand/runs` as sensitive even after scrubbing.
- Do not disable scrubbing for real customer workflows.
- Add custom rules before promoting a baseline if inspect shows domain-specific
  values you do not want stored.

## References

- [Structural scrub pipeline](scrub-structural-pipeline.md)
- [Detector library](scrub-detector-library.md)
- [Session hashing](scrub-session-hashing.md)
- [SQLite local store](sqlite-local-store.md)
