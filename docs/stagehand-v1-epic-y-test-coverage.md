# Epic Y Test Coverage

This document maps Epic Y's custom tool behavior tests to the bugs they are intended to catch. It is a coverage map, not a second implementation plan.

## Current Coverage

| ID    | Behavior                                           | Python                                  | TypeScript                         | Go                                                              |
| ----- | -------------------------------------------------- | --------------------------------------- | ---------------------------------- | --------------------------------------------------------------- |
| Y-T1  | Replay determinism, back-to-back                   | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T2  | Replay isolation from live implementation/network  | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T3  | Cross-service assertion positive case              | n/a                                     | n/a                                | `internal/analysis/assertions/evaluate_test.go`                 |
| Y-T4  | Cross-service assertion negative case              | n/a                                     | n/a                                | `internal/analysis/assertions/evaluate_test.go`                 |
| Y-T5  | Strict 5x tool error-injection determinism         | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T6  | Tool error-injection control case                  | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T7  | Tool argument scrubbing                            | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T8  | Tool result scrubbing                              | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T9  | Distinguishable same-tool call ordering            | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T10 | Async tool function capture/replay                 | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T11 | Tool replay returns recorded value, not live value | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T12 | Tool replay raises recorded error                  | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T13 | Tool replay fails closed on missing call           | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | n/a                                                             |
| Y-T14 | Nested tool parent/child linkage                   | `sdk/python/tests/test_tool_wrapper.py` | `sdk/typescript/src/index.test.ts` | `cmd/stagehand/inspect_test.go`                                 |
| Y-T15 | Model/tool/API timeline ordering                   | n/a                                     | n/a                                | fixture-shape guard in `examples/custom-tool-demo/main_test.go` |

## Notes

- Go does not implement a tool-wrapper SDK. Go coverage focuses on artifact analysis, assertion semantics, inspect rendering, and the custom tool demo's persisted fixture shape.
- Python and TypeScript SDK tests are the source of truth for wrapper behavior: replay, fail-closed behavior, scrubbing, async handling, nested calls, and tool error injection.
- The custom tool demo constructors intentionally create inspectable fixture runs. Tests against those constructors are named as fixture-shape guards so they are not mistaken for runtime SDK replay tests.

## Remaining High-Value Gap

The remaining higher-effort test is an artifact-driven integration harness that runs a Python or TypeScript tool scenario as a subprocess, persists the capture through the CLI/SQLite path, then reads it with `internal/store/sqlite`.

That test should verify:

- real SDK-produced tool interactions persist with `protocol: tool`
- model/tool/API sequence ordering survives persistence
- scrub reports survive persistence
- inspect/diff/assert consume those persisted interactions

This is useful, but it should be implemented as a small integration harness rather than as another fixture-only Go test.
