# Stagehand Python SDK

This package is the Python SDK for Stagehand.

Implementation status:

- bootstrap API exists through `stagehand.init(session, mode, config_path=None)`
- runtime guard prevents double initialization in one process
- run and session metadata are exposed through the runtime object for later recorder hooks
- `httpx` sync and async requests are intercepted at the client `send` layer
- captured interactions are normalized into the core interaction shape and exposed from the runtime in memory
- timeout and generic HTTP errors are captured as events instead of being dropped
- current D2 capture is pre-scrub and pre-persistence; it is not yet a safe stored artifact
- OpenAI chat-completion requests are detected as provider-aware operations
- OpenAI SSE chunks are captured in order, including tool-call boundary events when present
- replay mode can return exact seeded OpenAI responses back through the same `httpx` calling surface
- current D3 replay is exact-match only and seeded from in-memory captured interactions, not persisted recordings yet
