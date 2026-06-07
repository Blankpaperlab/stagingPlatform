# Security Posture And Threat Model

Stagehand is a local-first record/replay tool for agent workflows. It can
observe model calls, provider calls, generic HTTP traffic, and wrapped tool
calls, then persist scrubbed recordings for replay, diffing, assertions, and CI
review.

This document defines the launch security posture for the current OSS product
shape. It is a living model, not a formal third-party audit.

## Posture Summary

Stagehand assumes recordings are sensitive engineering artifacts.

The current product posture is:

- local-first by default
- scrub-before-persist in the standard durable recording path
- fail-closed replay when a recorded interaction cannot be matched
- explicit user configuration for capture scope, scrub rules, and replay
  behavior
- no hosted Stagehand service required for core record, replay, diff,
  assertion, or CI workflows

Stagehand does not claim that scrubbed artifacts are public-safe. Users should
treat `.stagehand/runs`, generated reports, uploaded CI artifacts, and
`inspect --show-bodies` output as internal data unless reviewed.

## Assets

The main assets this model protects are:

- provider credentials such as OpenAI and Stripe API keys
- authorization headers, session tokens, JWTs, cookies, and idempotency keys
- customer and end-user data captured in requests, responses, events, tool
  arguments, tool results, and tool errors
- proprietary prompts, internal IDs, business labels, and workflow decisions
- local replay baselines and diffs that reveal application behavior
- session-scoped scrub salts and the local master key used to protect them

## In Scope

This model covers the current repository behavior:

- Go CLI and local SQLite store
- Python SDK capture and replay paths
- TypeScript SDK capture and replay paths
- local `.stagehand` run storage and reports
- repository GitHub Action workflow behavior
- docs and examples that handle live provider credentials

## Out Of Scope

The current launch posture does not yet cover a production hosted Stagehand
backend, hosted UI, hosted multi-tenant storage, hosted authentication, hosted
retention controls, or managed customer data processing.

Those surfaces are future epics. They require a separate data model, auth model,
tenant isolation model, retention policy, and operational security review before
launch.

## Trust Boundaries

### User Machine

The user machine is the primary trust boundary for local mode. Stagehand assumes
the developer or CI job running the CLI is authorized to run the target agent and
access its environment variables.

Data crossing into Stagehand from the local process includes:

- environment variables passed to the managed subprocess
- SDK runtime bootstrap values
- observed HTTP and provider requests
- observed responses and stream events
- wrapped tool call inputs and outputs

Stagehand should not be run against workflows whose data cannot be stored as
internal local engineering artifacts, even after scrubbing.

### Managed Agent Subprocess

`stagehand record` and `stagehand replay` run a managed command and inject
Stagehand-specific environment variables so SDKs can capture or replay
interactions.

The managed subprocess remains application code. Stagehand does not sandbox it,
prevent filesystem access, or prevent arbitrary network access outside supported
interception paths. Users should run only trusted harnesses and should keep the
first captured scenarios narrow.

### SDK Interception Layer

The Python and TypeScript SDKs intercept supported SDK, HTTP, streaming, and
tool-wrapper paths inside the process. Unsupported clients can bypass capture.

Security implication:

- captured paths can be scrubbed and replayed through Stagehand
- unsupported paths may still make live network calls
- `stagehand doctor` and preflight checks reduce this risk but do not create a
  sandbox

### Local Persistence

The local SQLite database and generated reports are stored under `.stagehand`.
The standard recording writer scrubs interactions before writing to SQLite.

The SQLite store intentionally persists what it receives. The scrub-before-
persist guarantee lives in the recorder-side writer, not in hidden database
mutation.

### GitHub Actions And CI

CI runs inherit the trust boundary of the repository workflow, runner, and
configured secrets. Stagehand reports and run artifacts uploaded from CI should
be treated as internal artifacts.

Workflow authors are responsible for scoping secrets, choosing whether artifacts
are uploaded, and avoiding execution of untrusted pull-request code with
privileged credentials.

### Hosted Services

No hosted Stagehand service is required for the current local-first workflow.
Until hosted APIs exist and are explicitly documented, users should assume
Stagehand does not provide hosted storage, hosted identity, tenant isolation, or
hosted retention guarantees.

## Threats And Current Controls

| Threat                                                                      | Current Controls                                                                                                                            | Residual Risk                                                                                          |
| --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Provider API keys are stored in recordings                                  | Default detectors scrub common authorization headers, API keys, JWTs, passwords, and secret-like fields before standard persistence         | Unknown key formats or unsupported capture paths may require custom scrub rules                        |
| Customer PII is stored in recordings                                        | Default detectors scrub emails, phone numbers, SSNs, credit cards, and secret-like values; custom structural rules can redact domain fields | Domain-specific identifiers, names, notes, and business labels may not be inferable automatically      |
| Raw capture bypasses scrub-before-persist                                   | Standard CLI durable recording path uses `internal/recording.Writer` to scrub before SQLite writes                                          | Low-level store APIs and nonstandard integrations must preserve the same write path                    |
| Replay accidentally reaches live providers                                  | Replay is designed to fail closed on misses in supported SDK paths                                                                          | Unsupported clients or application side effects outside Stagehand wrappers can still call live systems |
| CI exposes sensitive reports                                                | Reports are explicit artifacts and comments configured by workflow authors                                                                  | Repository permissions, fork settings, and artifact retention remain user responsibilities             |
| Local `.stagehand` data is read by another local user or process            | Local mode keeps data on the user's machine and supports run/session deletion semantics                                                     | Stagehand does not currently encrypt the full SQLite database or reports at rest                       |
| Session salt compromise allows correlating scrubbed values inside a session | Salts are session-scoped and encrypted before storage; plaintext salt material is only needed in memory                                     | A compromised local machine or master key can expose salt material                                     |
| Hosted data is exposed                                                      | Hosted storage is not part of the current required workflow                                                                                 | Future hosted work needs independent auth, tenancy, retention, and disclosure controls                 |

## Secret Handling

Stagehand secret handling is based on three rules:

1. Avoid durable raw secrets in the standard path.
2. Preserve deterministic replay and assertions without storing plaintext secret
   values.
3. Make remaining risk visible to users.

Current behavior:

- provider keys and other credentials are read from the user's existing
  environment by the target process
- Stagehand config examples use placeholders, not real credentials
- standard durable recording scrubs before SQLite persistence
- scrub reports list changed paths so users can inspect what was redacted
- session-scoped deterministic replacements allow related values to match inside
  one session
- scrub salts are session-scoped and stored encrypted
- `DeleteSession` removes runs, runtime session state, queue state, and the
  associated session scrub salt

User responsibilities:

- do not commit `.env`, `.stagehand`, generated reports, or run artifacts that
  contain internal data
- do not disable scrubbing for real customer workflows
- add custom scrub rules for domain-specific sensitive fields
- inspect new baselines before sharing or uploading broadly
- scope CI secrets to trusted workflows and trusted code

## Local Risk Assumptions

Local mode assumes:

- the developer machine or CI runner is trusted enough to execute the agent
  harness
- the user controls the `.stagehand` directory and artifact retention
- the user understands that scrubbed recordings can still reveal sensitive
  business behavior
- the target harness is trusted code, not an arbitrary untrusted program
- replay should prefer correctness and fail closed over silently calling live
  services

Local mode does not assume:

- full local disk encryption provided by Stagehand
- OS-level sandboxing of the target process
- complete capture of every possible network library
- automatic classification of every domain-specific sensitive value

## Hosted Risk Assumptions

Hosted Stagehand is not part of the current required product path. Before hosted
storage, shared dashboards, or multi-user APIs launch, the project must define
and implement:

- tenant isolation boundaries
- authentication and authorization rules
- hosted run and baseline retention defaults
- deletion and export behavior
- audit logging expectations
- encryption-at-rest and key-management posture
- vulnerability disclosure and incident response process
- rules for processing customer data in support workflows

Until those controls exist, hosted documentation should not imply that users can
upload sensitive recordings to a Stagehand-managed service.

## Operational Guidance

For launch usage:

- keep Stagehand local-first unless there is an explicit reason to upload
  artifacts
- run `stagehand doctor` and `stagehand preflight` before promoting a baseline
- inspect the first baseline with `stagehand inspect --show-bodies`
- use custom scrub rules for internal IDs, tenant names, free-text notes, and
  proprietary labels
- store CI artifacts only where repository policy allows internal data
- prefer narrow deterministic scenarios for first baselines
- delete obsolete runs or sessions when they are no longer needed

## Related Docs

- [Scrubbing](scrubbing.md)
- [Retention and deletion](retention-deletion.md)
- [SQLite local store](sqlite-local-store.md)
- [Limitations](limitations.md)
- [First-run onboarding](first-run-onboarding.md)
