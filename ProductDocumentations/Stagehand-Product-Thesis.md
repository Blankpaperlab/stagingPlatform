# Stagehand Product Thesis

## One-Line Thesis

Stagehand is the local-first staging and regression layer for AI agents: it records real agent runs, scrubs sensitive data, replays workflows offline, compares behavior against baselines, and turns agent reliability into a repeatable CI practice.

## Core Argument

AI agents are moving from demos into production workflows, but most teams still test them with tools built for either ordinary deterministic code or isolated prompt evaluation. That leaves a dangerous gap.

An agent run is not just a prompt and a response. It is a timeline of model calls, streamed output, tool calls, internal HTTP requests, third-party APIs, retries, failures, state changes, and business decisions. A small prompt or code change can alter which system the agent calls, the order of operations, whether a side effect happens, or how sensitive data moves through the workflow.

Stagehand's thesis is that production agent reliability needs an execution staging layer, not just a prompt scorecard. Teams should be able to capture one known-good workflow, protect the data inside it, replay it without hitting live services, inspect what changed, and enforce expectations before a pull request merges.

Stagehand turns agent behavior into an engineering artifact.

## The Problem

Teams shipping AI agents face a reliability problem that conventional testing does not fully solve.

Unit tests can prove helper functions, but they rarely validate the full interaction between the model, tools, APIs, and side effects. Prompt evaluations can catch some output regressions, but they usually miss whether the agent called the right service in the right order. Live smoke tests are expensive, flaky, and risky when agents touch customer data, billing, support workflows, refunds, tickets, or internal systems. Hand-written mocks take time to maintain and often fail to preserve the exact evidence needed for debugging.

The result is that teams often ship agent changes without being able to answer basic release questions:

- Did this change alter the order of tool calls?
- Did the agent call billing before verifying the customer?
- Did a replay accidentally hit a live provider?
- Did fallback behavior hide a regression?
- Did sensitive data get persisted in a local artifact?
- Did the agent still recover from a simulated provider failure?
- Can CI explain the behavior change in a way a developer can act on?

Stagehand exists to make those questions answerable.

## Product Promise

The first product promise is intentionally narrow:

Record one important agent workflow, scrub it, replay it offline, compare it against a known-good baseline, and catch behavior drift in CI.

That promise matters because it maps directly to how engineering teams already ship software. Stagehand does not ask teams to replace their agent framework, move sensitive data to a hosted dashboard, or rewrite every dependency behind a mock layer. It adds a staging loop around existing Python and TypeScript agents.

## Target User

The primary user is an AI platform engineer, product engineer, infrastructure-minded founder, or developer working on production agent workflows.

This user typically has:

- Python or TypeScript agents
- OpenAI-compatible model calls
- internal HTTP APIs such as CRM, billing, support, fulfillment, or admin services
- third-party APIs such as Stripe
- custom tools that wrap business logic
- GitHub Actions or another CI workflow
- concern about credentials, customer data, and replay artifacts
- pressure to ship prompt and workflow changes faster without breaking production behavior

The first buyer is not looking for a broad no-code QA suite. They want a practical developer tool that makes agent behavior reproducible, inspectable, and enforceable.

## Core Workflow

The Stagehand product loop is:

1. Initialize Stagehand in an existing repo.
2. Record a representative agent workflow.
3. Scrub sensitive values before durable local persistence.
4. Promote the known-good run as a baseline.
5. Replay the workflow offline after code, prompt, or tool changes.
6. Diff the replay against the baseline.
7. Evaluate assertions about calls, ordering, payloads, forbidden operations, and fallback use.
8. Inspect failures locally from the terminal.
9. Run the same checks in CI.

This creates a baseline discipline for agents. Expected behavior changes are promoted intentionally. Unexpected behavior drift fails fast.

## Why Local-First

Stagehand is local-first because agent recordings are sensitive engineering material.

A real agent run can contain provider credentials, proprietary prompts, customer PII, internal account identifiers, support conversations, billing events, tool schemas, business decisions, and operational traces. A hosted-first product would need to win a large amount of trust before the user could even try the core workflow.

Stagehand lowers that adoption barrier by keeping the first useful experience on the developer's machine and CI runner. Teams can record, replay, inspect, diff, assert, delete, and manage baselines without sending raw agent traces to a hosted service.

Hosted collaboration may become valuable later, but it should come after the local reliability loop is proven. The wedge is trust first, collaboration second.

## Product Shape

Stagehand is best understood as an agent staging platform. It combines five roles:

- a recorder for real agent interactions
- a local artifact store for runs, events, baselines, scrub reports, replay evidence, and runtime state
- a replay system for deterministic offline reruns
- a regression engine for diffs, assertions, conformance checks, and failure injection
- a developer workflow exposed through a CLI, SDKs, examples, docs, and GitHub Actions

The current product surface supports that identity through a Go CLI, a local SQLite store, Python and TypeScript SDKs, scrub and detector pipelines, baseline promotion, diffing, assertions, conformance checks, error injection, and examples for realistic agent workflows.

## Differentiation

Stagehand is different because it focuses on the execution timeline around an agent, not only the final model output.

Compared with prompt evaluation tools, Stagehand checks external behavior: API calls, tool calls, ordering, errors, side effects, fallback use, and replay drift.

Compared with generic HTTP recorders, Stagehand is built for agent workflows. It understands model-provider patterns, streaming events, tool boundaries, scrub provenance, baselines, assertions, and CI reporting.

Compared with hand-built mocks, Stagehand preserves evidence from real runs and makes drift visible without requiring teams to manually recreate every service boundary.

Compared with hosted observability tools, Stagehand starts before production and works locally, which is better aligned with sensitive pre-release regression testing.

The strongest insight is that agent reliability is not one feature. It is a loop: capture, protect, replay, compare, enforce, debug, and intentionally update.

## Strategic Wedge

Stagehand should win by being narrow and reliable before becoming broad.

The strongest initial wedge is:

- Python and TypeScript agents
- OpenAI-compatible model calls
- internal HTTP APIs
- Stripe-like side-effecting workflows
- custom tools
- local SQLite persistence
- terminal inspection
- GitHub Actions enforcement

This is enough to serve teams whose agents are graduating from prototype to production-critical workflow. It avoids the trap of trying to simulate every SaaS provider or build a hosted collaboration suite before the core reliability loop is trusted.

## Security Thesis

Stagehand treats recorded artifacts as sensitive by default.

The product should continue to prioritize:

- local-first execution
- scrub-before-persist behavior
- default protection for authorization and cookie headers
- detector-based scrubbing for common secrets and PII
- custom scrub rules for domain-specific data
- session-scoped deterministic hashing for replay-safe identifiers
- explicit deletion commands for runs and sessions
- fail-closed replay behavior where supported
- honest documentation about unsupported capture paths and residual risk

Stagehand should not claim that scrubbed artifacts are public-safe. The stronger and more credible claim is that Stagehand gives teams a disciplined local workflow for reducing sensitive data exposure while making agent behavior testable.

## Product Principles

1. Local-first before hosted-first.
2. Scrub before durable persistence.
3. Replay should fail closed when it cannot match behavior safely.
4. Baselines should be explicit and intentional.
5. Diffs and assertions should point to concrete evidence.
6. Unsupported capture paths should be visible.
7. Simulators should be narrow and trustworthy before they are broad.
8. CI output should be both machine-readable and human-readable.
9. Examples should reflect real workflows, not toy-only demos.
10. Security posture should be explicit, testable, and conservative.

## Main Risks

Stagehand's main risks are product focus and trust.

Capture coverage risk: unsupported clients can bypass capture and weaken replay confidence.

Security risk: users may over-trust scrubbed artifacts or miss sensitive domain-specific fields.

Replay strictness risk: exact replay can feel brittle if configuration, ignore rules, and fallback explanations are weak.

Simulator scope risk: trying to support too many providers too early can reduce quality.

Onboarding risk: developers need a fast path from install to first replay, or the tool will feel heavy.

Category risk: buyers may confuse Stagehand with prompt evals, observability, or generic mocking unless the staging narrative stays sharp.

These risks are manageable if Stagehand keeps the first promise tight and invests in docs, examples, `doctor`, `preflight`, CI ergonomics, and clear failure messages.

## Definition Of Success

Stagehand succeeds when a team can:

1. Add Stagehand to an existing agent repo.
2. Record one representative workflow.
3. Verify sensitive values are scrubbed.
4. Promote the run as a baseline.
5. Make a code, prompt, or tool change.
6. Replay offline without hitting live services.
7. See a useful diff or assertion failure.
8. Add the same check to CI.
9. Trust the baseline enough to use it in normal release work.

At that point, Stagehand is no longer a demo recorder. It becomes reliability infrastructure for agentic software.

## Conclusion

Stagehand's product thesis is that AI agents need a staging layer built around full execution behavior. Prompt outputs alone are not enough. Production logs are too late. Hand mocks are expensive and incomplete. Live smoke tests are risky for every change.

The right abstraction is a local-first record, replay, diff, assertion, and CI loop for real agent workflows.

If Stagehand executes this thesis well, it can become the default tool teams reach for when an agent moves from prototype to production-critical work.
