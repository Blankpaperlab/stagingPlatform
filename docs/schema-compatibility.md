# Schema Validation and Compatibility

## Purpose

This document defines the validation strategy and compatibility policy for Stagehand schemas in Story `B3`.

It applies to:

- config files loaded by `internal/config`
- recording artifacts loaded by `internal/recorder`

Current schema versions:

- config schema: `v1alpha1`
- artifact schema: `v1alpha1`

## Validation Strategy

Stagehand V1 uses a code-first validation strategy.

The source of truth is:

- Go struct definitions
- strict decoders
- explicit `Validate()` methods
- fixture-based tests

V1 does not use external JSON Schema or OpenAPI files as the authoritative validator.

### Config validation

Config files are validated in two stages:

1. strict YAML decode with `KnownFields(true)`
2. semantic validation through `Validate()`

This means config validation fails on:

- unknown fields
- invalid enum values
- contradictory settings
- missing required values

### Artifact validation

Artifact files are validated in two stages:

1. strict JSON decode with `DisallowUnknownFields()`
2. semantic validation through `Run.Validate()`

This means artifact validation fails on:

- unknown fields
- invalid status or protocol values
- missing required metadata
- ordering violations
- invalid incomplete/corrupted run state

## Validation Fixtures

Fixture-based contract tests are part of the schema surface.

Fixture locations:

- config fixtures: `internal/config/testdata/`
- artifact fixtures: `internal/recorder/testdata/`

These fixtures are intended to answer two questions:

1. what is considered valid today
2. what must be rejected today

Whenever schema behavior changes, fixtures must change with it.

## Version Format

Schema versions use a major-first format:

- `v<major>alpha<iteration>` during alpha

Examples:

- `v1alpha1`
- `v1alpha2`

The meaning is:

- `major`: breaking contract generation
- `alpha iteration`: pre-stable revision within the same major line

V1 currently operates entirely in the `alpha` phase.

## Current Runtime Rule

Because the current schemas are alpha:

- exact version match is required at runtime

That means:

- `v1alpha1` readers accept `v1alpha1`
- `v1alpha1` readers reject `v1alpha2`
- `v1alpha1` readers reject `v2alpha1`

This is intentional. The product is still shaping the contract and should fail fast instead of pretending compatibility that does not exist.

## Minor-Version Compatibility Rules

For this project, a minor schema iteration means:

- `v1alpha1` -> `v1alpha2`

Policy:

1. Writers always emit the current schema version only.
2. Readers are never assumed to be forward-compatible.
3. Same-major backward minor compatibility is allowed only after it is implemented explicitly and covered by fixtures.
4. During alpha, same-major minor versions are still treated as incompatible unless exact-match support is broadened intentionally.

If a future release wants to claim same-major backward compatibility, it must satisfy all of these:

- the change is additive or migration-backed
- previous-minor fixtures are preserved and tested
- the loader has explicit compatibility or migration logic
- the compatibility claim is documented in this file and the affected schema doc

## Major-Version Migration Expectations

A major version bump means the old and new schema contracts are not safely interchangeable.

When a major version changes:

- the loader must reject older major versions by default, or
- a dedicated migrator must be shipped

If a migrator is shipped, expectations are:

- migration is explicit, not silent magic
- migration is one major step at a time
- migrated output validates against the new schema
- fixtures cover both pre-migration input and post-migration output

V1 does not yet ship general migration tooling. The current expected behavior for major mismatches is hard failure with an actionable error.

## What Can Change Without a Schema Version Bump

Allowed without a schema version bump:

- documentation clarifications
- comments and examples
- internal refactors that do not change serialized input or output
- stricter rejection of inputs that were already invalid under the documented rules
- new tests and fixtures for already-documented behavior

Not allowed without a schema version bump:

- adding a persisted field
- removing a persisted field
- renaming a persisted field
- changing field meaning or requiredness
- changing enum values
- changing ordering or lifecycle semantics in a way that makes previously valid data invalid

In short:

- serialized contract changes require a schema version bump

## Release Discipline

Before changing either schema:

1. update the relevant code-first types
2. update validation logic
3. update fixtures
4. update `docs/config-schema.md` or `docs/artifact-schema.md`
5. update this compatibility document if the compatibility promise changed

If any of those steps are skipped, the schema change is incomplete.
