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
| Q    | Design partner recruiting and feedback ops             |       8d | M2-M6                  |
| R    | Distribution work                                      |       7d | M5-M6                  |

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

| Milestone | Dates               | Primary Epics | Supporting Epics | Blockers   | Exit Condition                                                         |
| --------- | ------------------- | ------------- | ---------------- | ---------- | ---------------------------------------------------------------------- |
| M0        | Apr 22-Apr 30, 2026 | A, B          | P                | A, B       | Schemas, license, threat model, repo bootstrap are complete            |
| M1        | May 1-May 20, 2026  | C, D, E, F    | P                | C, D, E, F | Safe Python OpenAI record/replay works with scrubbed persistence       |
| M2        | May 21-Jun 17, 2026 | G, H, N       | Q                | H, N       | Runtime foundation works and TS first slice is usable                  |
| M3        | Jun 18-Jul 15, 2026 | I             | H, K, Q          | I, H       | Stripe subset and failure testing demo work end to end                 |
| M4        | Jul 16-Aug 12, 2026 | J, K, L       | Q                | J, K, L    | Assertions, diff, and CI reporting are production-shaped               |
| M5        | Aug 13-Sep 9, 2026  | M, O, R       | P, Q             | M, O       | Conformance, examples, docs, and launch assets are ready               |
| M6        | Sep 10-Oct 7, 2026  | O, P, Q, R    | M                | O, P, Q    | OSS launch is complete and the feedback sprint has closed top blockers |

### Full-Scope Plan B Extension

| Milestone | Dates                     | Primary Epics | Supporting Epics | Blockers                            | Exit Condition                                                  |
| --------- | ------------------------- | ------------- | ---------------- | ----------------------------------- | --------------------------------------------------------------- |
| M7        | Oct 8-Oct 28, 2026        | S, T          | Q                | S or T only if demanded by partners | Dedicated Gmail and Slack coverage exists if validated by users |
| M8        | Oct 29-Nov 25, 2026       | U             | Q                | U                                   | Postgres scope is usable enough for one real partner workflow   |
| M9        | Nov 26-Dec 23, 2026       | V, W          | X                | V                                   | Hosted backend and basic dashboard work for external teams      |
| M10       | Dec 24, 2026-Jan 20, 2027 | X             | Q, R             | X                                   | Hosted beta is stable enough to onboard early teams             |

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
| Q    | M2    | M6  | Starts once there is something demoable                 |
| R    | M5    | M6  | Launch prep and launch execution                        |
| S    | M7    | M7  | Plan B only                                             |
| T    | M7    | M7  | Plan B only                                             |
| U    | M8    | M8  | Plan B only                                             |
| V    | M9    | M9  | Plan B only                                             |
| W    | M9    | M9  | Plan B only                                             |
| X    | M9    | M10 | Plan B only                                             |

## Blocker Rules

These are the epics that should stop milestone rollover if they are late.

| Epic | Why It Blocks                                                  |
| ---- | -------------------------------------------------------------- |
| B    | No stable schema means downstream work churns immediately      |
| E    | Persisting before scrubbing is a security failure              |
| H    | Runtime weakness breaks Stripe, assertions, and CI credibility |
| I    | Without Stripe plus failure injection, the wedge is too weak   |
| K    | No baseline logic means diff and CI are half-real              |
| M    | Shipping simulators without conformance invites silent drift   |
| O    | No examples or docs means launch converts poorly               |
| P    | No retention/deletion/disclosure posture fails security review |

## Can-Slip Rules

These epics can slip one milestone without invalidating the product, as long as the slip is explicit.

| Epic | Slip Rule                                                                                                 |
| ---- | --------------------------------------------------------------------------------------------------------- |
| G    | TypeScript can trail Python by one milestone if Python adoption starts first                              |
| N    | Generic HTTP can narrow to exact replay only if nearest-neighbor matching is unstable                     |
| Q    | Partner recruiting can start light in M2, but cannot be absent by M5                                      |
| R    | Distribution assets can be rough at first, but the demo video and launch post must exist before M6 starts |
| S    | Only build if partner demand is concrete                                                                  |
| T    | Only build if partner demand is concrete                                                                  |
| V    | Hosted can slip indefinitely without invalidating Plan A                                                  |
| W    | Dashboard can slip behind hosted API if CLI remains the primary UX                                        |

## Board Setup Recommendation

If you turn this into a project board, use these columns:

1. `Milestone 0`
2. `Milestone 1`
3. `Milestone 2`
4. `Milestone 3`
5. `Milestone 4`
6. `Milestone 5`
7. `Milestone 6`
8. `Plan B Deferred`

Use labels:

- `epic:A` through `epic:X`
- `critical-path`
- `security`
- `launch`
- `design-partner`
- `can-slip`

## Immediate Next Mapping

If work starts now, the first active epics should be:

1. A: repo scaffold
2. B: config and event schemas
3. P: threat model, license, retention/deletion defaults

Do not open D before B exists.
Do not persist recordings from D until E exists.
Do not start hosted or Postgres epics unless Plan B is explicitly chosen.
