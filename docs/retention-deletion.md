# Retention And Deletion

Stagehand V1 is local-first. Stored runs, baselines, runtime session state, and
generated reports remain local unless a user or CI workflow uploads them.

## Retention Defaults

Default retention is manual retention.

Stagehand does not automatically expire local recordings in V1. Data under
`.stagehand` remains until one of these happens:

- a user deletes a run with `stagehand delete-run`
- a user deletes a session with `stagehand delete-session`
- a user removes the local storage directory directly
- a CI workflow or artifact provider expires uploaded artifacts according to
  that provider's configured retention policy

Generated reports under `.stagehand/reports` are not currently pruned by the
deletion commands. Treat reports as internal artifacts and remove obsolete report
files when they are no longer needed.

## Delete One Run

Use `delete-run` when a single stored run should be removed while keeping the
session available.

```bash
stagehand delete-run --run-id <run-id>
```

With an explicit config path:

```bash
stagehand delete-run --run-id <run-id> --config stagehand.yml
```

Current behavior:

- deletes the run row
- deletes dependent interactions and events
- deletes baselines that reference the deleted run
- preserves the session scrub salt
- preserves other runs in the same session

The session scrub salt is preserved because deterministic replacements must
remain stable for other runs in that session.

## Delete One Session

Use `delete-session` when the whole session should be removed.

```bash
stagehand delete-session --session <name>
```

With an explicit config path:

```bash
stagehand delete-session --session <name> --config stagehand.yml
```

Current behavior:

- deletes all runs in the session
- deletes dependent interactions and events
- deletes baselines that reference those runs
- deletes runtime session records and snapshots
- deletes session clocks and scheduled runtime events
- deletes the session scrub salt

Deleting a session is the supported way to remove the session-scoped scrub salt.

## Missing Data

Both commands fail if the requested run or session is not found. This makes
automation explicit: a successful command means something was actually deleted.

## Local Storage Path

Both commands use `record.storage_path` from `stagehand.yml`. If the configured
path is a directory, Stagehand uses `stagehand.db` inside that directory. If the
configured path ends in `.db`, `.sqlite`, or `.sqlite3`, Stagehand uses that file
directly.

## CI Artifacts

Stagehand cannot delete artifacts that were already uploaded to GitHub Actions
or another CI provider. Configure artifact retention in the workflow or provider
settings, and avoid uploading reports from untrusted pull-request contexts with
privileged secrets.

## Related Docs

- [Security posture](security-posture.md)
- [SQLite local store](sqlite-local-store.md)
- [Baseline and CI usage](baseline-ci.md)
