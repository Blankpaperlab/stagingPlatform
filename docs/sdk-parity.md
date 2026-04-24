# SDK Parity

This page defines the current cross-language parity bar between the Python and TypeScript SDKs.

## What Is Covered

The shared parity suite uses common fixture scenarios under `testdata/sdk-parity/` and validates that both SDKs emit equivalent artifact-shaped interactions for the same local HTTP flows.

Current shared fixtures:

- `http-get-success.json`
- `http-get-timeout.json`
- `openai-chat-success-default-host.json`
- `openai-chat-success-custom-host.json`
- `openai-sse-success.json`
- `http-post-scrub-fixture.json`

Current parity assertions:

- service, operation, protocol, streaming, and `fallback_tier` align
- request method, request path, selected headers, and normalized request-body shape align
- event ordering aligns
- success responses align on status code, selected headers, and decoded body
- OpenAI SSE capture aligns on `stream_chunk`, `stream_end`, `tool_call_start`, and `tool_call_end` ordering plus selected event payload fields
- timeout flows align on terminal event type
- in-memory scrub provenance aligns on:
  - `scrub_policy_version: v0-unredacted`
  - `session_salt_id: salt_pending`
- the shared persisted writer path is exercised against the scrub parity fixture so request/response header rules and `redacted_paths` behavior are verified once the capture shape reaches Go persistence

## What Is Not Covered Yet

These are known parity gaps, not hidden failures:

- TypeScript request-body capture for body-bearing `fetch` / `undici` requests is still less expressive than Python in some Node runtime paths, so shared parity currently normalizes body-bearing request shapes to `{"capture_kind":"present"}` instead of asserting decoded request-body equality.
- TypeScript streaming replay parity currently covers supported SSE flows with exact chunk re-delivery and fail-closed handling for malformed seeded streams, but it is still limited to the currently recognized OpenAI-style SSE capture shapes.

## Test Locations

- Python parity tests: `sdk/python/tests/test_parity.py`
- TypeScript parity tests: `sdk/typescript/src/index.test.ts`
- Shared fixtures: `testdata/sdk-parity/`
