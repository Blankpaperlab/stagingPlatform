# Epic Y Test Plan

## Purpose

This plan hardens Epic Y beyond shape checks. Each test is intended to fail when a real behavior regresses: replay determinism, replay isolation, tool assertions, tool error injection, scrubbing, ordering, async wrappers, nested links, and the custom tool demo.

## Test Philosophy

Every test must be designed so that a non-obvious bug in the underlying capability would cause it to fail. Tests that pass whether the capability works or not are documentation, not regression coverage.

Each implementation should include a short `// Catches:` or test comment that names the class of bug it protects against.

## Inventory

| ID    | Name                                             | Catches                                                        | Target area                                      |
| ----- | ------------------------------------------------ | -------------------------------------------------------------- | ------------------------------------------------ |
| Y-T1  | Replay determinism, back-to-back                 | Non-deterministic replay ordering or payload selection         | `examples/custom-tool-demo`                      |
| Y-T2  | Replay isolation, offline                        | Replay accidentally hitting live network                       | Python/TS SDK replay plus demo fixtures          |
| Y-T3  | Cross-service assertion, positive case           | Assertion passes without producing semantic equality evidence  | `internal/analysis/assertions`                   |
| Y-T4  | Cross-service assertion, negative case           | Assertion passes when linked IDs differ                        | `internal/analysis/assertions`                   |
| Y-T5  | Error injection determinism, 5x repeat           | Deterministic tool injection behaving probabilistically        | SDK tool injection and Go injection engine       |
| Y-T6  | Error injection control case                     | Injection firing when disabled or unmatched                    | SDK tool injection                               |
| Y-T7  | Tool argument scrubbing                          | Sensitive tool args persisted in plaintext                     | Python and TypeScript tool wrappers              |
| Y-T8  | Tool result scrubbing                            | Sensitive tool results persisted in plaintext                  | Python and TypeScript tool wrappers              |
| Y-T9  | Tool argument ordering preserved across replay   | Tool replay serving the right values in the wrong order        | Tool replay stores                               |
| Y-T10 | Async tool function                              | Async decorators/wrappers losing capture boundaries            | Python and TypeScript tool wrappers              |
| Y-T11 | Tool replay returns recorded value               | Replay calling the live function instead of recorded output    | Python and TypeScript tool wrappers              |
| Y-T12 | Tool replay raises recorded error                | Replay swallowing or transforming recorded errors incorrectly  | Python and TypeScript tool wrappers              |
| Y-T13 | Tool replay fail-closed on missing call          | Replay inventing results for unrecorded tool calls             | Python and TypeScript tool wrappers              |
| Y-T14 | Nested tool call from model tool-call handler    | Parent/child interaction linkage missing                       | Tool artifact schema and inspect                 |
| Y-T15 | Multi-call ordering across model/tool/API events | Sequence assignment wrong across interleaved interaction types | Capture buffers, demo fixtures, and inspect/diff |

## Implementation Order

1. Add helper assertions and fixture builders for `examples/custom-tool-demo`.
2. Implement Y-T3 and Y-T4 in `internal/analysis/assertions` first, because they are pure Go and validate the `tool` selector evidence.
3. Implement Y-T5 and Y-T6 in Python and TypeScript tool-wrapper tests, because the SDK wrappers own injected runtime behavior.
4. Implement Y-T7 through Y-T13 in Python and TypeScript wrapper tests.
5. Implement Y-T1, Y-T9, Y-T14, and Y-T15 against the custom tool demo artifact fixtures.
6. Add Y-T2 replay-isolation tests to the SDKs using strict blocked transports.

## Test Details

### Y-T1: Replay Determinism

Goal: record once, replay twice from the same recorded artifact, then compare interactions after removing run-local fields.

Bug caught: replay keyed by map iteration, scheduling, mutable shared replay queues, or any unstable ordering.

Acceptance checks:

- replay A and replay B have the same interaction count
- service, operation, protocol, request body, terminal event type, terminal data, and sequence match exactly
- run-local fields such as `run_id`, `interaction_id`, timestamps, and latency are ignored

Recommended target:

- `examples/custom-tool-demo/main_test.go`
- helper: `canonicalReplayInteractions(run recorder.Run) []map[string]any`

### Y-T2: Replay Isolation

Goal: replay succeeds while all live network dispatch is blocked and counted.

Bug caught: replay falling through to OpenAI, internal APIs, Stripe, or support APIs on misses.

Acceptance checks:

- replay returns expected interactions
- blocked transport attempt count is `0`
- replay miss test proves the transport would fail if called

Recommended targets:

- Python: use a failing `httpx.MockTransport`
- TypeScript: use an Undici `MockAgent` or blocking dispatcher

### Y-T3: Cross-Service Assertion Positive Case

Goal: `lookup_customer` result ID equals the later billing API `customer_id`.

Bug caught: assertion engine pattern-matching fields without checking semantic equality.

Acceptance checks:

- assertion status is `passed`
- `Evidence.LinkedEntities` has exactly one matching pair
- evidence includes the concrete shared value, for example `cus_test_42`

Recommended target:

- `internal/analysis/assertions/evaluate_test.go`

### Y-T4: Cross-Service Assertion Negative Case

Goal: lookup returns `cus_test_42`, billing sends `cus_test_99`, and the assertion fails.

Bug caught: assertion engine passing mismatched entity links.

Acceptance checks:

- assertion status is `failed`
- left evidence includes `cus_test_42`
- right evidence includes `cus_test_99`
- linked entity count is `0`

Recommended target:

- `internal/analysis/assertions/evaluate_test.go`

### Y-T5: Error Injection Determinism

Goal: run the same tool-injection config five times and verify the same call fails each time.

Bug caught: probability or call counters behaving nondeterministically when the config is deterministic.

Acceptance checks:

- each run has the same number of lookup calls
- call 1 succeeds
- call 2 fails with `CustomerNotFoundError`
- call 3 succeeds
- run metadata has exactly one applied injection

Recommended targets:

- `sdk/python/tests/test_tool_wrapper.py`
- `sdk/typescript/src/index.test.ts`

### Y-T6: Error Injection Control Case

Goal: run the same flow without a matching injection rule and verify all tool calls succeed.

Bug caught: injection always firing regardless of config.

Acceptance checks:

- no lookup call fails
- metadata has no `error_injection.applied`
- downstream refund/support behavior follows the non-injected path

### Y-T7: Tool Argument Scrubbing

Goal: pass sensitive values through tool arguments and verify persisted artifacts never contain the original plaintext.

Bug caught: scrub report says data was redacted but persistence still contains sensitive values.

Acceptance checks:

- serialized capture bundle or persisted SQLite interaction JSON does not contain the original email, token, card, or secret
- `scrub_report.redacted_paths` includes the relevant `request.body.arguments.*` paths
- replay matching still works using scrubbed arguments

Recommended targets:

- Python and TypeScript tool-wrapper tests
- optional helper: serialize `runtime.capture_bundle_dict()` or `snapshotCapturedInteractions()`

### Y-T8: Tool Result Scrubbing

Goal: return sensitive data from a tool and verify terminal event results are scrubbed before persistence.

Bug caught: tool results bypassing the scrubber.

Acceptance checks:

- persisted/captured interaction JSON does not contain the original sensitive result
- `scrub_report.redacted_paths` includes `events[1].data.result.*`
- replay returns the persisted scrubbed value consistently

### Y-T9: Tool Args Ordering Preserved Across Replay

Goal: record multiple calls to the same tool with distinguishable arguments, then replay in the same order.

Bug caught: replay store serving the correct tool name but wrong occurrence.

Acceptance checks:

- replayed calls preserve `cus_001`, `cus_002`, `cus_003` order
- sequence values are strictly increasing
- a reordered seed fixture fails or returns mismatched values

### Y-T10: Async Tool Function

Goal: capture and replay actual async tool functions, not sync functions invoked from async tests.

Bug caught: async wrapper losing await boundaries, parent links, or replay behavior.

Acceptance checks:

- two async calls record two interactions
- replay returns recorded results without incrementing a live-call counter
- error path raises the recorded async error

Current baseline:

- Python has async tool coverage.
- TypeScript should keep Promise-returning tool coverage alongside sync tests.

### Y-T11: Tool Replay Returns Recorded Value

Goal: use a live counter tool to prove replay returns recorded values and does not call the implementation.

Bug caught: replay invoking the real function and coincidentally returning a plausible value.

Acceptance checks:

- record returns `1`, `2`, `3`
- replay returns `1`, `2`, `3`
- live counter remains unchanged during replay

### Y-T12: Tool Replay Raises Recorded Error

Goal: record a tool error and verify replay raises the recorded class/message.

Bug caught: replay swallowing, flattening, or transforming recorded errors incorrectly.

Acceptance checks:

- record raises expected error
- replay raises expected error class/name and message
- live implementation is not called during replay

### Y-T13: Tool Replay Fail-Closed On Missing Call

Goal: record two calls, replay three calls, and require the third to fail closed.

Bug caught: replay inventing missing results.

Acceptance checks:

- first two replay calls succeed with recorded values
- third replay call raises the SDK replay-miss type
- live implementation is not called for the third call

### Y-T14: Nested Tool Call From Model Tool-Call Handler

Goal: model interaction emits a tool-call boundary and the local tool interaction is linked as a child.

Bug caught: missing `parent_interaction_id` or `nested_interaction_id` linkage.

Acceptance checks:

- child tool has `parent_interaction_id` equal to model interaction ID
- parent event has `nested_interaction_id` equal to tool interaction ID
- `inspect --show-bodies` renders the tool under the model interaction

Recommended target:

- `cmd/stagehand/inspect_test.go` or custom tool demo fixtures

### Y-T15: Multi-Call Ordering Across Boundaries

Goal: verify exact sequence across model, tool, internal API, second tool, Stripe, and support-ticket interactions.

Bug caught: sequence assignment races or boundary-specific capture ordering bugs.

Acceptance checks:

- expected sequence:
  - `1` `https` `openai`
  - `2` `tool` `stagehand.tool` / `lookup_customer`
  - `3` `https` `internal-billing`
  - `4` `tool` `stagehand.tool` / `lookup_customer`
  - `5` `https` `stripe`
  - `6` `https` `support-ticket-api`
- no duplicate sequence values
- sequence order matches timeline order in inspect output

## Helper Infrastructure

Add helpers only as tests require them:

- `filterByTool(interactions []recorder.Interaction, toolName string) []recorder.Interaction`
- `findInteractionByServiceOperation(interactions []recorder.Interaction, service, operation string) *recorder.Interaction`
- `terminalData(interaction recorder.Interaction) map[string]any`
- `pathValue(interaction recorder.Interaction, path string) (any, bool)` or reuse assertion evaluator helpers where appropriate
- `canonicalInteractionForDeterminism(interaction recorder.Interaction) map[string]any`
- `containsPath(paths []string, target string) bool`
- SDK-specific blocked network helpers:
  - Python: `httpx.MockTransport` that counts attempts and raises
  - TypeScript: Undici dispatcher or `MockAgent` that rejects unexpected requests

Avoid adding a `RawInteractionBlobs` store API unless a test genuinely needs SQLite-level byte inspection. Prefer serializing the persisted `recorder.Interaction` rows returned by `GetRun` first; add raw-store inspection only if a bug could hide behind structured decoding.

## Coverage Matrix

| Story                               | Coverage                               |
| ----------------------------------- | -------------------------------------- |
| Y1 Python tool wrapper              | Y-T7, Y-T8, Y-T10, Y-T11, Y-T12, Y-T13 |
| Y2 TypeScript tool wrapper          | Y-T7, Y-T8, Y-T10, Y-T11, Y-T12, Y-T13 |
| Y3 Tool artifact schema and inspect | Y-T9, Y-T14, Y-T15                     |
| Y4 Tool assertions                  | Y-T3, Y-T4                             |
| Y5 Tool error injection             | Y-T5, Y-T6                             |
| Y6 Custom tool demo                 | Y-T1 through Y-T6, Y-T9, Y-T14, Y-T15  |

Y-T1 and Y-T2 are cross-cutting replay safety tests and should stay in CI even if the demo implementation changes.

## Known Gaps

These are not launch blockers for V1, but should be tracked for V1.1:

- long-running sessions with hundreds of tool calls
- concurrent custom tool calls
- Python/TypeScript artifact parity for identical tool flows
- real provider tool-call format drift
- latency and throughput regressions

## CI Guidance

After the 15 tests are implemented:

```bash
go test ./examples/custom-tool-demo ./internal/analysis/assertions ./cmd/stagehand -count=50
go test ./examples/custom-tool-demo ./internal/analysis/assertions ./cmd/stagehand -race
python -m pytest sdk/python/tests/test_tool_wrapper.py -q
npm run -w @stagehand/sdk test
npm run ci:all
```

The `-count=50` run is intended to catch determinism and ordering flakes. Race detection should focus on Go packages that own demo fixtures, assertion evaluation, artifact rendering, and any shared sequencing logic.
