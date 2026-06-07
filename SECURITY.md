# Security Policy

Stagehand is a local-first record/replay tool that can observe provider calls,
HTTP traffic, tool inputs, tool outputs, and replay artifacts. Treat recordings,
reports, local SQLite stores, and CI artifacts as sensitive engineering data even
after scrubbing.

## Supported Versions

Stagehand is currently pre-1.0. Security fixes are targeted at the latest
released alpha and the `main` branch. Older alpha snapshots are not guaranteed to
receive backports.

## Reporting A Vulnerability

Do not report security issues in public GitHub issues, discussions, or pull
requests.

Use GitHub private vulnerability reporting or a private repository security
advisory when available. If private reporting is not available for the repository,
open a public issue that asks for maintainer security contact without including
technical details, exploit steps, secrets, logs, recordings, or proof-of-concept
payloads.

Please include privately:

- affected version or commit
- affected component, such as CLI, SDK, scrubber, replay, CI, or examples
- impact and realistic attack or leak scenario
- reproduction steps with synthetic data
- whether real credentials, customer data, or run artifacts may have been exposed

Expected response target:

- acknowledge within 5 business days
- confirm severity and affected surface after reproduction
- coordinate a fix, release note, and credit if desired

## Security Scope

In scope for launch:

- scrub-before-persist behavior in the standard durable recording path
- plaintext secret or PII persistence in local `.stagehand` artifacts
- replay paths that unexpectedly call live services despite fail-closed behavior
- dependency or CI configuration issues that could expose secrets
- vulnerabilities in the Go CLI, Python SDK, TypeScript SDK, local SQLite store,
  examples, and GitHub Action wrapper

Out of scope for the current OSS launch:

- hosted Stagehand service behavior, because no hosted service is part of the
  current required workflow
- bugs caused by intentionally disabling scrubbing or bypassing the standard
  recording writer
- unsupported client libraries that are documented as not intercepted
- disclosure of synthetic credentials used in tests or examples

## Handling Sensitive Artifacts

When reporting a vulnerability, do not send raw `.stagehand` directories,
SQLite databases, CI artifacts, provider logs, customer data, or real API keys
unless maintainers explicitly request them through a private channel.

Use synthetic recordings whenever possible.
