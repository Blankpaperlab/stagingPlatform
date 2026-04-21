# Stagehand V1 Build Plan

## Purpose

This is the revised build plan. It does not just restate the architecture. It makes scope cuts, calls out where the original V1 spec is too ambitious for one engineer, adds missing operational workstreams, and converts the project into a schedule that can survive contact with reality.

Companion doc: `stagehand-v1-epic-milestone-map.md` maps epics to milestones and shows which epics are blocking versus can-slip.

The core conclusion is simple:

- The current full-scope V1 is not a 6-month solo build.
- If you keep everything in the current spec, the realistic timeline is 8-9 months.
- If you want a real 6-month V1, you need to cut scope hard.

## Hard Recommendation

There are two viable plans. Pick one deliberately.

### Plan A: Real 6-Month V1

Ship the local-first product only:

- Python SDK
- TypeScript SDK
- Go CLI
- SQLite
- OpenAI/Anthropic simulator
- Stripe simulator
- Generic HTTP fallback simulator
- Scrubbing
- Assertions
- Diff engine
- GitHub Action
- Error injection
- Basic conformance harness
- Three first-party example flows

Defer from V1:

- Postgres wire protocol
- Hosted beta
- Proxy mode
- Slack and Gmail as standalone high-fidelity simulators unless design partners prove they matter more than TS parity or Stripe depth

This is the recommended plan.

### Plan B: Keep the Current Spec

Keep everything in the current architecture:

- Postgres wire protocol
- Gmail and Slack dedicated simulators
- Hosted beta
- Team collaboration
- Dashboard
- Multi-tenant Postgres backend

This is not a 6-month solo V1. It is an 8-9 month build, assuming the engineer is unusually strong and scope discipline remains high.

If you choose Plan B, the schedule in this document must be treated as the baseline, not the original 6-month target.

## Where I Disagree With the Current V1 Spec

These are not formatting opinions. These are build-risk opinions.

### 1. Postgres Wire Protocol Does Not Belong in a Solo 6-Month V1

This is the biggest disagreement.

Postgres wire interception is deep systems work. It is not just another simulator. It has protocol complexity, state semantics, query/result fidelity, failure modes, and debugging burden. For one engineer, it competes directly with the product's actual wedge: record/replay for AI-agent external behavior with failure testing in CI.

Recommendation:

- Defer Postgres wire protocol to V1.1 or V2.
- If database coverage is required earlier, instrument common Python and Node DB libraries first.
- If even that is too much, let databases remain outside replay for initial design partners and document it clearly.

### 2. Hosted Beta Is Too Early Unless Scope Is Tiny

A hosted backend plus auth plus tenancy plus dashboard plus shared baselines is its own product surface. Building it while stabilizing the OSS core is a predictable way to end up with two half-built products.

Recommendation:

- Hosted should not block OSS release.
- If hosted must exist in the first 9 months, reduce it to:
  - run list
  - single-run timeline
  - baseline selection
- Defer org/team administration, deep links, and polished diff UX until after design partner usage proves demand.

### 3. Six High-Fidelity Simulators Is Too Many for a First Release

Simulator count is not the metric. Useful coverage is the metric.

Recommendation:

- Make OpenAI/Anthropic, Stripe, and Generic HTTP the real V1 service pack.
- Gmail and Slack should start either as thin wrappers over generic HTTP behavior or as limited side-effect views, not full fidelity targets.
- Only build dedicated Gmail/Slack simulators if design partners depend on them.

### 4. The Local Daemon Should Be Optional at First

A daemon may be right eventually. It is not required for the first convincing slice.

Recommendation:

- First slice should work as library + CLI + SQLite.
- Introduce `stagehandd` only when sessions, snapshots, and concurrent replay orchestration actually need it.

### 5. Scrubbing Is a Security Boundary, Not a Supporting Feature

The original architecture recognizes this, but the build sequencing still treated it like something that could trail the first real recording flow. That is wrong.

Recommendation:

- No recording is persisted before scrubbing exists.
- If you need an earlier proof of concept, it must print to stdout only and explicitly avoid disk persistence.

### 6. Licensing Should Not Be Split Across OSS Components in V1

A mixed MIT-for-SDK and Apache-for-core strategy adds noise for little benefit.

Recommendation:

- Use one permissive license across the OSS codebase.
- Apache 2.0 is the cleaner default because it is enterprise-friendly and has patent protection.
- Keep hosted product and future enterprise-only features proprietary.

### 7. Compatibility Policy Must Be Explicit

If recordings break every release, trust erodes immediately.

Recommendation:

- Add `schema_version`, `sdk_version`, and `runtime_version` to every recording artifact.
- Support replay compatibility for at least the previous two minor versions.
- Provide a `stagehand migrate-recording` command.
- Permit breaking changes only on major versions.

## Product Goal

Stagehand is successful when an AI platform engineer can:

1. Add one line of SDK init.
2. Record a real agent run locally.
3. Replay it deterministically offline.
4. Inject failures.
5. Assert behavior in CI.
6. Diff against a baseline.
7. Debug the timeline from the CLI.

If this loop is not reliable, nothing else matters.

## Scope Freeze

### In Scope for Recommended V1

- Python SDK
- TypeScript SDK
- CLI
- SQLite local storage
- Ordered event recording
- Scrubbing with session-scoped consistency
- Replay runtime
- Fallback tiers 0-2
- Optional tier 3 LLM fallback behind explicit config
- Error injection
- Assertion engine
- Diff engine
- GitHub Action
- OpenAI/Anthropic support
- Stripe support
- Generic HTTP support
- Conformance harness for shipping simulators
- Three example agent flows

### Explicitly Deferred from Recommended V1

- Postgres wire protocol
- HTTPS proxy
- Hosted dashboard
- Hosted team collaboration
- Gmail dedicated simulator
- Slack dedicated simulator
- Production-traffic imports other than file import
- Multi-agent orchestration
- On-prem
- SSO/SAML/SCIM
- gRPC

### If You Insist on Keeping Full Scope

Treat these as V1 features but accept:

- 8-9 month schedule
- lower confidence in quality
- slower user feedback loop
- higher security and operational burden before PMF

## Delivery Strategy

Build this as three layers, in order:

1. Core loop
   - record
   - scrub
   - persist
   - replay
   - inspect

2. Trust layer
   - assertions
   - diffs
   - error injection
   - conformance

3. Expansion layer
   - more simulators
   - hosted collaboration
   - proxy mode

If the core loop is weak, the trust layer will expose that weakness and the expansion layer will just multiply support burden.

## Workstreams

This project is not one engineering track. It is five tracks that must run in parallel with different intensity.

### Workstream A: Core Product Engineering

- SDKs
- recorder
- runtime
- storage
- simulators
- assertions
- diff
- CLI

### Workstream B: Security and Data Handling

- threat model
- scrubbing
- retention policy
- deletion flows
- dependency scanning
- vuln disclosure policy
- secret handling

### Workstream C: Conformance and Test Reliability

- contract tests
- integration tests
- nightly conformance
- flakiness control
- compatibility tests

### Workstream D: Design Partner and Feedback Loop

- recruit 3-5 design partners
- run onboarding sessions
- collect blocked workflows
- prioritize fixes against roadmap

### Workstream E: Distribution

- docs
- example apps
- landing page
- demo video
- launch post
- outbound outreach list

If you ignore D and E until after engineering is "done," you will launch into a vacuum.

## Critical Path

These are the items that actually determine the schedule:

1. Event schema freeze
2. Scrubbing before persistence
3. Python SDK exact record/replay
4. SQLite stability
5. Replay runtime with session isolation
6. Stripe simulator and error injection
7. Assertions and diffs
8. CI integration
9. Conformance coverage for shipped simulators
10. Example flows and docs

Hosted, proxy, and Postgres are not on the recommended critical path.

## Dependency Graph

This is the missing dependency model.

### Hard dependencies

- Event schema blocks recorder, replay, diff, assertions, and compatibility work.
- Scrubbing blocks any persisted recording workflow.
- SQLite store blocks CLI record/replay demo.
- Session manager blocks snapshots, fork/restore, and realistic replay.
- Entity extraction blocks cross-service assertions.
- Stripe simulator blocks compelling failure-injection demo.
- Diff engine depends on stable run alignment and baseline semantics.
- GitHub Action depends on replay, assertions, and diff output.
- Conformance harness depends on stable simulator contracts.

### Soft dependencies

- TypeScript parity can lag Python by 2-3 weeks without breaking the initial launch narrative.
- Dedicated Gmail and Slack simulators can slip if Generic HTTP covers their examples.
- Hosted beta can slip without invalidating OSS V1.

### Can-slip items

- dashboard polish
- proxy mode
- deep multi-tenant features
- extensive provider-specific TS wrappers beyond required paths

## Ticket Estimates

These are rough engineer-day estimates for one strong solo engineer. They are intentionally not optimistic.

| Epic | Scope                                                  | Estimate | Notes                                           |
| ---- | ------------------------------------------------------ | -------: | ----------------------------------------------- |
| A    | Repo scaffold, CI, packaging skeleton                  |       4d | Includes Go/Python/TS workspace setup           |
| B    | Config schemas and event schemas                       |       5d | Must be written before broad implementation     |
| C    | SQLite store and migrations                            |       4d | Includes contract tests                         |
| D    | Python SDK first slice (`httpx`, OpenAI)               |       8d | Record + exact replay + SSE                     |
| E    | Scrubbing engine core                                  |      10d | Structural rules, detectors, salts, reports     |
| F    | CLI first slice (`record`, `replay`, `inspect`)        |       4d | Enough for first demo                           |
| G    | TypeScript SDK first slice                             |       8d | `fetch`, `undici`, minimal provider hooks       |
| H    | Runtime core: sessions, snapshots, queue, fallback 0-2 |      12d | This was under-estimated previously             |
| I    | Stripe simulator + error injection                     |      12d | Subset only, not full Stripe                    |
| J    | Assertion engine                                       |       6d | All six types, but cross-service stays shallow  |
| K    | Diff engine + baseline logic                           |       6d | JSON + terminal + CI-friendly markdown          |
| L    | GitHub Action                                          |       3d | CLI wrapper only                                |
| M    | Conformance harness first pass                         |       7d | OpenAI and Stripe only in initial V1            |
| N    | Generic HTTP simulator                                 |       4d | Exact + nearest-neighbor                        |
| O    | Example flows + docs + packaging                       |       8d | This is real work, not polish                   |
| P    | Security hardening for OSS launch                      |       6d | Threat model, retention, deletion, disclosure   |
| Q    | Design partner recruiting + feedback ops               |       8d | Spread across the project, not one block        |
| R    | Distribution work                                      |       7d | Demo video, launch post, landing page, outreach |

Total for recommended V1:

- 122 engineer-days
- Roughly 24-26 working weeks after allowing for bug fixing, context switching, and support load

That is why the recommended V1 is a real 6-month build only after cutting hosted and Postgres.

### Additional Estimates for Full-Scope Plan B

| Epic | Scope                           | Estimate | Notes                                 |
| ---- | ------------------------------- | -------: | ------------------------------------- |
| S    | Gmail dedicated simulator       |       6d | Minimal but real                      |
| T    | Slack dedicated simulator       |       6d | Web API only                          |
| U    | Postgres wire protocol          |      18d | Still probably optimistic             |
| V    | Hosted API + auth + tenancy     |      12d | Minimal viable hosted backend         |
| W    | Dashboard                       |      10d | Run list, detail, diff, assertions    |
| X    | Hosted ops and integration bugs |      10d | This is where schedules usually break |

Additional full-scope cost:

- 62 engineer-days
- Roughly 12-13 more weeks

That is why the full spec is an 8-9 month build, not a 6-month build.

## Milestones and Real Dates

Current date in this workspace is April 20, 2026. If you want this to stop being planning theater, use actual dates.

### Recommended Start Date

- Start Milestone 0 on April 22, 2026.

### Recommended Plan A Schedule

#### Milestone 0: Spec Freeze and Repo Bootstrap

- Dates: April 22, 2026 to April 30, 2026
- Outputs:
  - repo scaffold
  - config schema
  - event schema
  - assertion schema
  - compatibility policy
  - threat model draft
  - license decision
- Demo:
  - sample config and sample recording validate against schemas

#### Milestone 1: First Safe Record/Replay Slice

- Dates: May 1, 2026 to May 20, 2026
- Outputs:
  - SQLite store
  - Python SDK first slice
  - scrubbing before persistence
  - CLI `record`, `replay`, `inspect`
  - OpenAI exact replay with SSE
- Demo:
  - record a real OpenAI call, store scrubbed artifact, replay offline

#### Milestone 2: Runtime Foundation and TypeScript Slice

- Dates: May 21, 2026 to June 17, 2026
- Outputs:
  - session manager
  - snapshots
  - event queue
  - fallback tiers 0-2
  - TypeScript SDK first slice
  - Generic HTTP simulator
- Demo:
  - Python and TS apps can record/replay the same conceptual flow

#### Milestone 3: Stripe and Failure Testing

- Dates: June 18, 2026 to July 15, 2026
- Outputs:
  - Stripe simulator subset
  - error injection engine
  - entity extraction
  - baseline logic
- Demo:
  - replay Stripe-backed flow with deterministic failure injection

#### Milestone 4: Assertions, Diff, and CI

- Dates: July 16, 2026 to August 12, 2026
- Outputs:
  - all six assertion types
  - diff engine
  - GitHub Action
  - machine-readable reports
- Demo:
  - PR check fails on assertion failure or fallback regression

#### Milestone 5: Conformance, Examples, and Launch Prep

- Dates: August 13, 2026 to September 9, 2026
- Outputs:
  - conformance harness for OpenAI and Stripe
  - onboarding, refund, and support-escalation examples
  - packaging and docs
  - landing page draft
  - launch video draft
- Demo:
  - full example flow from record to CI diff

#### Milestone 6: Public OSS Launch and Feedback Sprint

- Dates: September 10, 2026 to October 7, 2026
- Outputs:
  - public OSS launch
  - issue triage
  - design partner onboarding
  - 2-week feedback-driven stabilization sprint
- Demo:
  - real external users successfully run the first example flow

This is the point where you decide whether to keep building solo, pursue design-partner revenue, or start fundraising conversations.

### Full-Scope Plan B Schedule

If you keep Postgres, Gmail, Slack, and hosted in scope:

- OSS core lands by October 2026
- Hosted beta and extra simulators land by December 2026 to January 2027

That is the honest version.

## Detailed Work by Milestone

### Milestone 0: Spec Freeze and Repo Bootstrap

#### Required decisions

These must be written down by April 30, 2026:

1. Artifact schema versioning policy
2. License choice
3. Baseline semantics
4. Fallback tier semantics
5. Error injection schema
6. Partial-run failure behavior
7. Deletion and retention defaults
8. Minimal threat model

#### Acceptance criteria

- Schemas validate sample artifacts.
- OSS license is chosen and written into the repo.
- Threat model exists in docs.
- Compatibility policy exists in docs.

### Milestone 1: First Safe Record/Replay Slice

#### Required tickets

- Implement `Run`, `Interaction`, `Event`, `ScrubReport`.
- Implement SQLite migrations.
- Implement Python bootstrap and `httpx` hook.
- Implement OpenAI request/stream capture.
- Implement scrub pipeline:
  - auth header drop
  - JSON field redaction
  - deterministic session hashing
  - scrub report generation
- Implement exact replay.
- Implement CLI commands:
  - `record`
  - `replay`
  - `inspect`

#### Acceptance criteria

- No persisted run contains plaintext auth headers.
- Replay works offline.
- Incomplete runs are marked as incomplete.
- `inspect` renders nested interaction trees if present.

### Milestone 2: Runtime Foundation and TypeScript Slice

#### Required tickets

- Implement session lifecycle.
- Implement snapshot and restore.
- Implement scheduled event queue.
- Implement fallback tier 1 matcher.
- Implement TypeScript `fetch` and `undici` interception.
- Implement Generic HTTP simulator.
- Add contract tests between Python/TS recordings and core runtime.

#### Acceptance criteria

- Sessions are isolated.
- Snapshots are restorable.
- Tier 1 fallback decisions are visible in artifacts.
- TS and Python both support the same config model.

### Milestone 3: Stripe and Failure Testing

#### Required tickets

- Implement Stripe object subset:
  - customer
  - payment method
  - payment intent or charge, but not both unless truly necessary
- Implement state consistency rules.
- Implement error library.
- Implement error injection matcher.
- Implement entity extraction for Stripe identities.
- Implement webhook scheduling for the subset supported.

#### Acceptance criteria

- Third-call decline injection works deterministically.
- State violations produce realistic errors.
- Baseline replay can forbid disallowed fallback tiers.

### Milestone 4: Assertions, Diff, and CI

#### Required tickets

- Implement YAML parser and schema validation.
- Implement all six assertion types.
- Implement run alignment.
- Implement diff report generation.
- Implement GitHub Action wrapper.
- Add artifacts and PR comment reporting.

#### Acceptance criteria

- Assertion failures name the exact interaction IDs involved.
- CI can fail on assertion errors and fallback regressions.
- Diff output is readable in terminal and GitHub markdown.

### Milestone 5: Conformance, Examples, and Launch Prep

#### Required tickets

- Implement conformance case model.
- Add OpenAI smoke conformance tests.
- Add Stripe smoke conformance tests.
- Create example apps:
  - onboarding
  - refund
  - support escalation
- Write docs:
  - getting started
  - scrubbing
  - error injection
  - limitations
- Create launch assets:
  - demo video
  - launch post draft
  - target outreach list

#### Acceptance criteria

- Nightly conformance runs complete for shipping simulators.
- At least three design partners have seen the demo before launch.
- Example flows act as regression tests.

### Milestone 6: OSS Launch and Feedback Sprint

#### Required tickets

- package OSS release
- publish vuln disclosure policy
- publish retention/deletion documentation
- run launch sequence
- triage launch issues
- prioritize partner-blocking issues

#### Acceptance criteria

- At least one external team gets to first replay without live assistance longer than one hour.
- The top 10 launch issues are categorized by:
  - bug
  - missing integration
  - docs gap
  - product gap

## Security and Data Plan

This was missing in the prior plan. It is now required.

### Threat model

Write this in week 1. Minimum threats to model:

- plaintext API keys written to disk
- leaked scrub salts
- replay artifacts containing recoverable PII
- malicious plugin or dependency compromise
- path traversal or arbitrary file access in import/export
- hosted tenant data isolation failures

### Security requirements for OSS launch

- dependency scanning in CI
- secret scanning in CI
- vuln disclosure policy published
- encrypted-at-rest storage for scrub salts
- retention defaults documented
- deletion command/API exists

### Retention and deletion defaults

Recommended defaults:

- local runs retained until user deletes them
- hosted runs default retention 30 days unless pinned
- scrub salts deleted with their sessions
- `stagehand delete-run` and `stagehand delete-session` exist in V1

If deletion is not supported in V1, the product will fail serious security review immediately.

## Versioning and Backward Compatibility

This was missing and must be explicit.

### Artifact metadata

Every recording artifact stores:

- `schema_version`
- `sdk_name`
- `sdk_version`
- `runtime_version`
- `scrub_policy_version`

### Compatibility policy

- Minor versions must replay artifacts from the previous two minor versions.
- Major versions may require migration.
- A migration command exists for stored artifacts.

### Why this matters

Otherwise CI baselines become brittle and every upgrade becomes a trust event.

## Product Resilience Plan

This was also missing.

### Failure modes to design for

- SDK process crash during capture
- daemon crash during session
- partial writes to SQLite
- replay mismatch due to corrupted artifact
- network blip during record
- simulator panic or unexpected state

### Required behaviors

- runs can be marked `complete`, `incomplete`, or `corrupted`
- incomplete runs are inspectable but not baseline-eligible by default
- writes are atomic at the interaction level
- replay emits precise failure reports instead of generic crashes
- import validates artifact integrity before accepting it

## Test Strategy and Reliability Targets

The previous test section was too generic. This one is actionable.

### Coverage targets

- `internal/scrub`: 95%+ line coverage
- `internal/runtime`: 85%+ line coverage
- `internal/analysis`: 85%+ line coverage
- storage contracts: 100% contract-case coverage
- simulator conformance: smoke coverage for every shipped simulator

### CI requirements

- unit and contract tests on every PR
- integration smoke tests on every PR
- nightly conformance on shipping simulators
- artifact compatibility tests on every release branch

### Flakiness budget

- PR test flake target: less than 1%
- conformance false positive target: less than 5% of nightly failures

If the suite gets flakier than that, stop feature work and fix it. This product cannot sell reliability while running flaky tests itself.

## Design Partner Plan

This needs to start before the product is "done."

### Start date

- Start recruiting by June 1, 2026.

### Target partners

- 3-5 AI product teams
- 20-150 person companies
- 1-5 production agents
- at least one team using Stripe or similar side-effecting APIs

### Goal by OSS launch

- at least 3 teams have agreed to try the OSS version
- at least 2 teams have told you which integrations they actually need next

### How feedback enters the roadmap

At the end of each milestone:

- collect blocked workflows
- label each issue as:
  - launch blocker
  - partner-specific
  - later
- reallocate up to 20% of the next milestone to partner-blocking issues

Without this explicit mechanism, "feedback" turns into either random context switching or total neglect.

## Distribution Plan

This is not optional work. It starts before launch.

### Start date

- Start launch prep by August 1, 2026.

### Required deliverables before OSS launch

- landing page
- 3-minute demo video
- launch blog post or Show HN draft
- screenshots and terminal demos
- outreach list of 50 AI engineers/founders

### Launch channels

- Show HN
- X/Twitter
- direct outreach to design partners and warm intros
- targeted posts in relevant AI engineering communities

## Fundraising Decision Gate

This should exist even if you plan to bootstrap.

### Decision point

- Review on October 15, 2026 after launch feedback sprint.

### Metrics to inspect

- number of active external teams
- weekly replay runs
- number of teams using CI integration
- number of teams asking for hosted mode
- number of teams blocked on integrations you deferred
- early revenue or design-partner commitments

### Decision paths

- If 3-5 teams are actively using it and hosted demand is clear:
  - build hosted minimal beta
  - consider a small raise or design-partner prepayments

- If usage is shallow but interest is high:
  - improve onboarding and docs first
  - do not rush into hosted

- If usage is weak and integration demands are fragmented:
  - narrow the wedge further instead of broadening scope

## License Decision

Make this in Milestone 0.

### Recommendation

- OSS core, SDKs, and CLI: Apache 2.0
- hosted service and future enterprise features: proprietary

### Reason

This is simpler than mixed OSS licenses across the repo and cleaner for enterprise adoption.

## Definition of Done by Subsystem

### SDK feature is done when

- it records and replays a documented example
- it passes recorder contract tests
- it documents unsupported cases
- it does not persist unsanitized secrets

### Simulator is done when

- it implements the simulator contract
- it isolates state by session
- it declares supported operations
- it has smoke conformance coverage
- it exposes named error cases if those matter to the service

### Assertion type is done when

- it validates schema input
- it passes positive and negative tests
- it reports concrete failing evidence

### CLI command is done when

- it has help output
- it supports machine-readable output where relevant
- it is exercised in integration tests

### Security feature is done when

- the threat it addresses is named in the threat model
- it has a regression test where possible
- the user-facing behavior is documented

## Spec Freeze Checklist

Broad implementation does not proceed until these are written and reviewed:

- config schema
- event schema
- assertion schema
- diff output schema
- simulator contract
- storage contract
- fallback semantics
- error injection schema
- scrub rule schema
- compatibility policy
- retention and deletion semantics
- CLI surface
- OSS license
- threat model

## Immediate Sequence

The old immediate sequence was wrong because it persisted recordings before scrubbing.

This is the corrected first 10-session sequence:

1. Create repo scaffold and choose license.
2. Write config and event schemas.
3. Implement `Run`, `Interaction`, `Event`, and schema validation.
4. Implement scrub pipeline skeleton.
5. Implement SQLite store.
6. Implement Python `httpx` interception.
7. Record an OpenAI call to memory only and verify scrubbed artifact shape.
8. Persist the scrubbed artifact to SQLite.
9. Implement exact replay and CLI `inspect`.
10. Record the first end-to-end example and turn it into a regression test.

## Release Criteria

Recommended V1 is ready when:

- Python and TypeScript both support a documented record/replay flow
- no persisted recording contains plaintext credentials in standard locations
- OpenAI/Anthropic, Stripe, and Generic HTTP ship with documented supported surfaces
- error injection works from config
- assertions and diffs work in CI via GitHub Action
- nightly conformance exists for every shipped simulator
- three example flows work end to end
- retention, deletion, compatibility, and disclosure policies are published

Full-scope V1 is ready when the above is true plus:

- Postgres is stable enough to be trusted by at least one real design partner
- hosted run viewing works for external teams
- Gmail and Slack dedicated simulators are usable enough to reduce support burden rather than increase it

## What You Should Actually Do Next

Stop polishing the spec after this revision.

If you want the honest answer to the implied question, it is this:

- Start Milestone 0 on April 22, 2026.
- Commit to Plan A unless a real design partner forces Plan B.
- Spend the first 30 days getting to a safe Python OpenAI record/replay demo.
- Do not build hosted or Postgres before external users prove they are blocking.

If you do not start coding this week, the bottleneck is no longer architecture quality. It is avoidance.
