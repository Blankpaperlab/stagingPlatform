# Product Documentation Index

## Purpose

This folder contains the current product documentation set for the Stagehand repository.

These documents describe:

- what files currently exist in the repo
- what code modules, types, functions, and entrypoints exist today
- what work has already been completed
- what is scaffolded versus what is truly implemented

Related implementation references under `docs/`:

- `config-schema.md`
- `artifact-schema.md`
- `schema-compatibility.md`
- `scrub-detector-library.md`
- `scrub-session-hashing.md`
- `sqlite-local-store.md`

## Document List

### `Tracked-File-Inventory.md`

Complete inventory of the current tracked source, config, workflow, and planning files in the repository, grouped by area.

### `Code-Modules-Types-and-EntryPoints.md`

Detailed documentation of the current code surface.

Important note:

- the current codebase does not use object-oriented classes as its main pattern
- the Go code is organized around packages, structs, constants, and functions
- the Python and TypeScript SDKs currently expose module-level functions and types rather than classes

This document therefore covers "classes" in the practical sense by documenting the actual types, exported constants, functions, and module entrypoints that exist.

### `Work-Completed-and-Current-State.md`

Current implementation status, completed stories, validated commands, and what remains scaffold-only.

## Scope Rules for These Docs

These documents describe the current repository state only.

They do not document:

- future code that does not exist yet
- generated directories like `node_modules/`, `bin/`, Python build artifacts, or cache folders as if they were product source
- imagined APIs that are still only present in planning docs

## Source of Truth

This documentation is secondary to the code.

If there is a conflict:

1. code and tests win
2. sample config files win over outdated prose
3. planning docs describe intended direction, not necessarily implemented behavior
