# Stagehand V1 Epic-to-Milestone Map

## Purpose

This is the companion planning page to `stagehand-v1-build-plan.md`. It answers one operational question only:

- which epics belong to which milestone

For story-level expansion of each epic, use `stagehand-v1-epic-breakdown.md`.

Use this doc when you are turning the build plan into tickets, a Kanban board, or a weekly execution review.

## How to Read This

- "Primary epics" are the epics that define the milestone.
- "Supporting epics" are active in the same window but are not the headline deliverable.
- "Ongoing epics" span multiple milestones and should stay open across the schedule.
- "Blockers" are the epics that must land for the next milestone to remain valid.

## Epic Legend

### Recommended Plan A Epics

| Epic | Scope                                                  | Estimate | Planned Milestone Span |
| ---- | ------------------------------------------------------ | -------: | ---------------------- |
| A    | Repo scaffold, CI, packaging skeleton                  |       4d | M0                     |
| B    | Config schemas and event schemas                       |       5d | M0                     |
| C    | SQLite store and migrations                            |       4d | M1                     |
| D    | Python SDK first slice (`httpx`, OpenAI)               |       8d | M1                     |
| E    | Scrubbing engine core                                  |      10d | M1                     |
| F    | CLI first slice (`record`, `replay`, `inspect`)        |       4d | M1                     |
| G    | TypeScript SDK first slice                             |       8d | M2                     |
| H    | Runtime core: sessions, snapshots, queue, fallback 0-2 |      12d | M2-M3                  |
| I    | Stripe simulator + error injection                     |      12d | M3                     |
| J    | Assertion engine                                       |       6d | M4                     |
| K    | Diff engine + baseline logic                           |       6d | M3-M4                  |
| L    | GitHub Action                                          |       3d | M4                     |
| M    | Conformance harness first pass                         |       7d | M5                     |
| N    | Generic HTTP simulator                                 |       4d | M2                     |
| O    | Example flows, docs, packaging                         |       8d | M5-M6                  |
| P    | Security hardening for OSS launch                      |       6d | M0, M1, M5-M6          |
| AB   | Agent behavior contract                                |       7d | M6                     |
| AC   | Action risk classifier                                 |       6d | M6                     |
| AD   | Release gates                                          |       7d | M6-M7                  |
| AE   | Agent release review command                           |       8d | M7                     |
| AF   | GitHub PR release review                               |       6d | M7                     |
| AG   | Approval workflow                                      |       5d | M7-M8                  |
| AH   | MCP action review first slice                          |       8d | M8                     |
| AI   | Launch demos for release review                        |       5d | M8                     |
| AJ   | Design partner validation                              |       6d | M6-M8                  |

### Full-Scope Plan B Extension Epics

| Epic | Scope                           | Estimate | Planned Milestone Span |
| ---- | ------------------------------- | -------: | ---------------------- |
| S    | Gmail dedicated simulator       |       6d | M7                     |
| T    | Slack dedicated simulator       |       6d | M7                     |
| U    | Postgres wire protocol          |      18d | M8                     |
| V    | Hosted API, auth, tenancy       |      12d | M9                     |
| W    | Dashboard                       |      10d | M9                     |
| X    | Hosted ops and integration bugs |      10d | M9-M10                 |

## Milestone-to-Epic Map

### Recommended Plan A

| Milestone | Dates                | Primary Epics | Supporting Epics | Blockers   | Exit Condition                                                             |
| --------- | -------------------- | ------------- | ---------------- | ---------- | -------------------------------------------------------------------------- |
| M0        | Apr 22-Apr 30, 2026  | A, B          | P                | A, B       | Schemas, license, threat model, repo bootstrap are complete                |
| M1        | May 1-May 20, 2026   | C, D, E, F    | P                | C, D, E, F | Safe Python OpenAI record/replay works with scrubbed persistence           |
| M2        | May 21-Jun 17, 2026  | G, H, N       | -                | H, N       | Runtime foundation works, TS first slice is usable, and custom APIs replay |
| M3        | Jun 18-Jul 15, 2026  | I             | H, K             | H          | Failure-readiness and baseline comparison are usable locally               |
| M4        | Jul 16-Aug 12, 2026  | J, K, L       | -                | J, K, L    | Assertions, diff, and CI reporting are ready to reframe as release review  |
| M5        | Aug 13-Sep 9, 2026   | O, P          | AA, Y            | O, P, AA   | Foundational docs, examples, onboarding, tools, and security are complete  |
| M6        | Sep 10-Oct 7, 2026   | AB, AC, AD    | AJ               | AB, AC     | Behavior contracts, risk classification, and first release gates exist     |
| M7        | Oct 8-Oct 28, 2026   | AE, AF        | AD, AG, AJ       | AE, AF     | Local review and GitHub PR review are the primary product workflow         |
| M8        | Oct 29-Nov 25, 2026  | AH, AI        | AG, AJ           | AI, AJ     | MCP action review, launch demos, and design partner validation are done    |

### Full-Scope Plan B Extension

| Milestone | Dates                     | Primary Epics | Supporting Epics | Blockers                            | Exit Condition                                                  |
| --------- | ------------------------- | ------------- | ---------------- | ----------------------------------- | --------------------------------------------------------------- |
| Post-validation | After AJ | S, T | AJ | S or T only if demanded by partners | Dedicated Gmail and Slack coverage exists if validated by users |
| Post-validation | After AJ | U    | AJ | U                                   | Postgres scope is usable enough for one real partner workflow   |
| Post-validation | After AJ | V, W | X  | V                                   | Hosted backend and basic dashboard work for external teams      |
| Post-validation | After AJ | X    | AJ | X                                   | Hosted beta is stable enough to onboard early teams             |

## Epic Span View

This is the reverse index: where each epic lives across the schedule.

| Epic | Start | End | Notes                                                   |
| ---- | ----- | --- | ------------------------------------------------------- |
| A    | M0    | M0  | One-time repo bootstrap                                 |
| B    | M0    | M0  | Must freeze before broad implementation                 |
| C    | M1    | M1  | Blocks first persisted demo                             |
| D    | M1    | M1  | Python-first wedge                                      |
| E    | M1    | M1  | Security boundary, not optional                         |
| F    | M1    | M1  | Minimal CLI only                                        |
| G    | M2    | M2  | Can trail Python slightly                               |
| H    | M2    | M3  | Split across runtime and replay-hardening work          |
| I    | M3    | M3  | Hardest simulator in recommended V1                     |
| J    | M4    | M4  | Depends on stable artifacts and entity extraction       |
| K    | M3    | M4  | Baseline logic starts before diff UI/reporting finishes |
| L    | M4    | M4  | Thin wrapper over CLI outputs                           |
| M    | M5    | M6  | First pass in M5, operational hardening in M6           |
| N    | M2    | M2  | Needed to keep simulator scope bounded                  |
| O    | M5    | M6  | Docs/examples continue through launch                   |
| P    | M0    | M6  | Starts with threat model, ends with launch hardening    |
| AB   | M6    | M6  | Behavior contract schema, generation, enforcement, diff |
| AC   | M6    | M6  | Risk labels for HTTP/API, tool, and database actions    |
| AD   | M6    | M7  | User-facing release gates over the assertion engine     |
| AE   | M7    | M7  | Main local `stagehand review` workflow                  |
| AF   | M7    | M7  | GitHub PR release review workflow                       |
| AG   | M7    | M8  | Explicit approval workflow for intentional changes      |
| AH   | M8    | M8  | MCP-style tool calls as reviewed actions                |
| AI   | M8    | M8  | Launch demos that sell release review                   |
| AJ   | M6    | M8  | Design partner validation before hosted/dashboard work  |
| S    | After AJ | After AJ | Plan B only after design partner validation       |
| T    | After AJ | After AJ | Plan B only after design partner validation       |
| U    | After AJ | After AJ | Plan B only after design partner validation       |
| V    | After AJ | After AJ | Plan B only after design partner validation       |
| W    | After AJ | After AJ | Plan B only after design partner validation       |
| X    | After AJ | After AJ | Plan B only after design partner validation       |

## Blocker Rules

These are the epics that should stop milestone rollover if they are late.

| Epic | Why It Blocks                                                           |
| ---- | ----------------------------------------------------------------------- |
| B    | No stable schema means downstream work churns immediately               |
| E    | Persisting before scrubbing is a security failure                       |
| H    | Runtime weakness breaks replay, gates, and CI credibility               |
| K    | No baseline logic means behavior review is half-real                    |
| J    | No assertion engine means release gates lack an execution layer         |
| L    | No GitHub Action means PR release review lacks its primary surface      |
| AB   | No behavior contract means there is no approved action baseline         |
| AC   | No risk classifier means review cannot explain why a new action matters |
| AE   | No `stagehand review` means the release-review workflow is not usable   |
| AF   | No PR review means the wedge is not visible where teams make releases   |
| AJ   | No real-user validation means hosted/dashboard work remains frozen      |

## Can-Slip Rules

These epics can slip one milestone without invalidating the product, as long as the slip is explicit.

| Epic | Slip Rule                                                                                                 |
| ---- | --------------------------------------------------------------------------------------------------------- |
| G    | TypeScript can trail Python by one milestone if Python adoption starts first                              |
| N    | Generic HTTP can narrow to exact replay only if nearest-neighbor matching is unstable                     |
| AD   | Some advanced gate syntax can slip if core `block_if` and approval gates work                             |
| AG   | Approval metadata polish can slip if explicit contract edits remain clear and auditable                   |
| AH   | MCP support can slip if non-MCP tool/API review is strong enough for the first design partners            |
| S    | Only build after design partner validation proves demand                                                  |
| T    | Only build after design partner validation proves demand                                                  |
| V    | Hosted can slip indefinitely without invalidating release-review V1                                       |
| W    | Dashboard can slip indefinitely while GitHub PR review remains the primary UX                             |

## Board Setup Recommendation

If you turn this into a project board, use these columns:

1. `Milestone 0`
2. `Milestone 1`
3. `Milestone 2`
4. `Milestone 3`
5. `Milestone 4`
6. `Milestone 5`
7. `Milestone 6`
8. `Milestone 7`
9. `Milestone 8`
10. `Plan B Deferred`

Use labels:

- `epic:A` through `epic:AJ`
- `critical-path`
- `security`
- `release-review`
- `design-partner`
- `can-slip`

## Immediate Next Mapping

If work starts now, the first active epics should be:

1. AB: agent behavior contract
2. AC: action risk classifier
3. AD: release gates
4. AE: agent release review command
5. AF: GitHub PR release review
6. AI: refund-risk launch demo
7. AJ: design partner validation

Do not open hosted/dashboard work, more provider simulators, or broad enterprise governance UI until at least three external teams have attempted the local or GitHub release-review workflow.
