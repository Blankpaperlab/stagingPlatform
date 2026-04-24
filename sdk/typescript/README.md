# Stagehand TypeScript SDK

This package is the TypeScript SDK for Stagehand.

Implementation status:

- bootstrap API exists through `init({ session, mode, configPath? })`
- CLI-aware bootstrap also exists through `initFromEnv()`
- supported SDK modes are `record`, `replay`, and `passthrough`; hybrid mode is not implemented
- runtime guard prevents double initialization in one process
- run and session metadata are exposed through the runtime object for later recorder hooks
- config autodiscovery follows `stagehand.yml` in the current working directory when present
- Node HTTP interception is installed automatically in `record` and `replay` modes
- built-in `fetch` traffic is captured through the shared `undici` dispatcher path
- direct `undici.fetch(...)` traffic is captured through the same interception layer
- captured interactions are normalized into the shared artifact-shaped structure and exposed through `runtime.snapshotCapturedInteractions()`
- network failures and aborted requests are captured as `error` and `timeout` terminal events
- OpenAI-style SSE responses now emit ordered `stream_chunk`, `stream_end`, `tool_call_start`, and `tool_call_end` events during capture
- additional OpenAI-compatible hosts can be supplied through `STAGEHAND_OPENAI_HOSTS=host1,host2`
- replay mode now supports exact-match seeded replay for non-streaming and supported SSE interactions
- replay misses fail closed before live network dispatch
- replay failures surface as `TypeError` rejections whose `cause` is a typed Stagehand replay error
- streaming exact replay is still not implemented in the TypeScript SDK
