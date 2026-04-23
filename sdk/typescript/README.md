# Stagehand TypeScript SDK

This package is the TypeScript SDK for Stagehand.

Implementation status:

- bootstrap API exists through `init({ session, mode, configPath? })`
- CLI-aware bootstrap also exists through `initFromEnv()`
- runtime guard prevents double initialization in one process
- run and session metadata are exposed through the runtime object for later recorder hooks
- config autodiscovery follows `stagehand.yml` in the current working directory when present
- request interception is not implemented yet
- replay integration is not implemented yet beyond env-driven bootstrap wiring
