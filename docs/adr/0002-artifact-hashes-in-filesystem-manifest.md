# Record artifact hashes in per-step filesystem manifests

Execution artifacts live only on the workspace volume: the API and web UI derive their list by walking the artifact directory, and no artifact metadata is persisted in the database. Adding a SHA-256 for each artifact therefore required a place to keep it. We chose a versioned JSON manifest per step, written atomically next to the step directories (`<execution>/<NNN>.json`) during collection, rather than a new table. The hash then lives with the bytes it describes, listing keeps its current live semantics, and no SQLite/Postgres migration is needed for data that disappears whenever files are removed by hand.

## Considered Options

- **`execution_artifacts` table**: queryable, but it duplicates the filesystem as a source of truth and leaves stale rows when artifacts are deleted outside pici.
- **Hash on demand while listing**: always consistent, but turns every page load into a full read of every artifact.
- **One `.sha256` sidecar per file**: simple to produce, but pollutes the artifact tree that pici itself lists.

## Consequences

- Executions collected before this change have artifacts without hashes; they are still listed and downloadable, but carry no `hash` field and no `ETag`.
- The manifest is only as durable as the artifact volume, and is not garbage-collected with the database.
