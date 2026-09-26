# Local Snapshot Executions — Design

## Context

pici runs every execution by cloning `project.RepoURL` at a ref/SHA
(`internal/ci/runner.go`). There is no way to run a workflow against uncommitted
local changes. This spec adds **snapshot executions**: the CLI uploads a `.tar.gz`
of the local worktree, and the server extracts it instead of cloning. The execution
stays tied to a pici project (variables, secrets, Docker cache, logs) but is
decorrelated from git — no push, no ref that must exist on the remote.

Decisions validated during brainstorming:

- **Snapshot upload only.** No server-less / 100% local execution mode.
- **Invocation.** `pici-cli run <project> <workflow> --local [--dir <path>]`.
- **Archive.** Worktree files (`git ls-files -c -o --exclude-standard`) plus
  `.git/` (minus `config`, `hooks/`, `logs/`).
- **Identity.** `ref` = local branch, `commit_sha` = local HEAD, marked
  `source=snapshot`.
- **No rebuild.** A snapshot execution cannot be rebuilt server-side.
- **Endpoint.** Reuse `POST /api/projects/{id}/executions`, branching on
  `Content-Type`.
- **Ordering.** Upload first, then enqueue — no race between the worker and the
  upload.

## Goals

- Run any workflow on the current worktree, including uncommitted and untracked
  (non-ignored) changes.
- Reuse the existing runner, image build, cache, variables, secrets, logs and
  artifacts unchanged.
- Keep JSON execution, webhooks, schedules, and every existing API/CLI client
  working exactly as before.
- Ingest the uploaded archive safely: nothing is ever written outside the
  execution's own workspace directory.

## Non-Goals

- A server-less / local execution mode.
- Rebuilding or re-running a snapshot execution on the server.
- Creating commits, pushing, or syncing a snapshot back to git.
- GitHub/GitLab check runs for snapshot executions.
- Incremental/delta uploads — each snapshot uploads a full archive.
- Archiving submodules as content (gitlink entries are sent as-is, like a
  normal checkout would see them).

## Architecture

- New `internal/archive` package: `ExtractTarGz(r io.Reader, dest string) error`,
  with hardened entry handling.
- `stores.Execution` gains a `Source` enum and an optional `SnapshotID`.
- `internal/handlers/executions.go` `Create` accepts `multipart/form-data` in
  addition to JSON.
- `internal/ci/runner.go` materializes the source: clone (git) or extract
  (snapshot).
- The `client` package and `cmd/pici-cli` gain a snapshot trigger and an archive
  builder.
- `internal/gc` cleans stale uploads; `internal/web` shows a "local" badge and
  hides the rebuild action.

## Data model

- `internal/stores/enums.go`:
  ```go
  // Source is where an execution's code comes from.
  // ENUM(git, snapshot)
  type Source string
  ```
  Generated with the existing `go-enum --marshal --sql --names` directive.
- Migration `004_execution_source` (both sqlite and postgres):
  - sqlite: `ALTER TABLE executions ADD COLUMN source TEXT NOT NULL DEFAULT 'git'
    CHECK (source IN ('git','snapshot'));` and `ADD COLUMN snapshot_id TEXT;`.
  - postgres: create `execution_source_enum AS ENUM ('git','snapshot')`, add
    `source execution_source_enum NOT NULL DEFAULT 'git'` and
    `snapshot_id uuid`.
- `Execution` fields:
  - `Source Source \`json:"source"\`` — exposed to API/UI/CLI.
  - `SnapshotID *uuid.UUID \`json:"-"\`` — opaque token linking the execution to
    its uploaded archive; never a client-supplied path.

## API

`POST /api/projects/{id}/executions` now accepts two content types:

- `application/json` `{workflow, ref}` → `source=git`, behavior unchanged.
- `multipart/form-data`:
  - `workflow` (required),
  - `ref` (optional),
  - `commit_sha` (optional),
  - `snapshot` (file `.tar.gz`, required) → `source=snapshot`.

Handling:

1. Resolve the project (`404` if missing).
2. Wrap the body in `http.MaxBytesReader` with
   `-max-snapshot-size` / `PICI_MAX_SNAPSHOT_SIZE` (default **512 MiB**);
   exceeding it returns `413`.
3. Validate `workflow` and the `snapshot` file (`400` otherwise).
4. Generate a `snapshot_id` (uuid) and stream the file to
   `<workspace>/uploads/<projID>/<snapshotID>.tar.gz` via a `.tmp` file followed
   by an atomic rename, so a partially received archive is never visible.
5. `Enqueue` the execution with `source=snapshot`, `snapshotID`, `ref`,
   `commitSHA`. If enqueue fails, delete the archive.
6. Return `202` with the execution JSON.

`POST .../executions/{executionID}/rebuild` on a snapshot execution returns
`409 Conflict` ("snapshot executions cannot be rebuilt").

## CLI

`pici-cli run <project> <workflow> --local [--dir <path>]`:

- `--dir` defaults to the current directory and must be inside a git worktree.
- **File list:** `git ls-files -c -o --exclude-standard -z` (tracked + untracked,
  ignoring `.gitignore`d paths). Files are read from disk, so uncommitted
  modifications are included. Symlinks are stored as symlinks (never followed).
- **`.git/`:** included, excluding `config`, `hooks/`, and `logs/`. `HEAD`,
  `refs/`, `objects/`, and `index` are kept so git commands and `PICI_COMMIT_SHA`
  work in the container.
- **Metadata:** `ref` from `git symbolic-ref --short HEAD` (fallback: short SHA
  when detached), `commit_sha` from `git rev-parse HEAD`.
- **Upload:** `multipart/form-data` via the `client` package; prints the
  execution ID like `run` does today. The CLI streams the archive and surfaces
  the server's `413` if it is too large; it does not enforce a separate
  client-side limit.

`logs`, `status`, and `cancel` are unchanged.

## Secure extraction

`archive.ExtractTarGz(r io.Reader, dest string)` applies, per entry:

- Verify the gzip/tar streams parse; reject on any decode error.
- Reject entries whose name is absolute or not local (`filepath.IsLocal`) and any
  resolved path that escapes `dest` (clean + prefix check).
- **Symlinks / hardlinks:** accept only when the resolved link target stays
  inside `dest`; otherwise error. Repos can legitimately contain symlinks, so
  they cannot simply be rejected.
- Reject other special types: device, char, block, and FIFO entries.
- **Anti tar-bomb:** bound the cumulative extracted bytes and the entry count
  (both derived from `-max-snapshot-size`), using an `io.LimitedReader` on the
  decompressed stream.
- Preserve regular file/dir modes, but strip `setuid`/`setgid`/sticky bits.

Extraction writes only under `dest` (the execution's `repoDir`), before any
container starts.

## Runner

In `runner.run()`, immediately after loading the project, materialize the source
based on `exec.Source`:

- `git` → current path (`git.Clone`), unchanged.
- `snapshot` → `archive.ExtractTarGz` from
  `<workspace>/uploads/<projID>/<snapshotID>.tar.gz` into `repoDir`, then recreate
  the remote (`git remote add origin <PICI_REPO_URL>` — `config` was excluded),
  then delete the archive. A failed extraction is a setup failure, exactly like a
  failed clone.

Everything downstream is reused as-is: `detectCaches`, Dockerfile build, steps,
cache binds, artifacts, logs. `resolveCommitSHA` / `resolveVersion` work because
`.git/HEAD` and `refs/` are present.

Snapshot-specific behavior:

- `createCheckRun` / `updateCheckRun` are skipped when `Source != git` (the local
  SHA may not exist on the remote).
- `SyncSchedule` is skipped (a local run does not register a cron).
- New env var `PICI_SOURCE` (`git` | `snapshot`); `PICI_REF` is the local branch
  and `PICI_COMMIT_SHA` the local HEAD, as sent by the CLI.
- `rebuild` is refused (see API).

## GC & UI

- **Runner:** deletes the upload right after extraction.
- **GC:** `internal/gc` gains cleanup of `<workspace>/uploads/**` for files older
  than `GCKeepDuration` (covers uploads orphaned when enqueue or extraction
  fails).
- **API/UI:** `source` is exposed in the execution JSON. The web UI shows a
  "local" badge on snapshot execution rows and detail pages, and hides the
  rebuild button for them.

## Error handling

- Missing project → `404`; missing `workflow` or file → `400`; over the size
  limit → `413`; rebuild of a snapshot → `409`.
- Upload failure → archive removed, execution not created (enqueue runs only
  after a complete upload).
- Extraction failure → execution failed with the setup log carrying the error,
  same as a clone failure.
- CLI side: not in a git worktree, empty file list, or archive too large → a
  clear error before any upload.

## Security

The archive is untrusted input. Mitigations:

- Path-jail for every entry; absolute/`..`/escaping paths rejected.
- Symlinks/hardlinks accepted only when they resolve inside `dest`.
- No special files; no setuid/setgid modes.
- Bounded decompressed size and entry count; gzip magic/stream validated.
- The `snapshot_id` is a server-generated opaque uuid; the client never supplies
  a path.
- `.git/config` is not uploaded (avoids leaking remote credentials embedded in
  URLs); the server regenerates the remote.
- Extraction is confined to the workspace and requires the same API token as any
  other execution trigger.

## Testing

- `internal/archive`:
  - rejects `../` traversal, absolute names, and paths escaping `dest`;
  - rejects symlinks whose target escapes `dest`, accepts in-tree symlinks;
  - rejects device/char/block/FIFO entries;
  - enforces the decompressed-size and entry-count bounds (tar bomb);
  - rejects invalid gzip;
  - strips setuid/setgid bits.
- `internal/handlers`: multipart create → `source=snapshot` and archive written;
  oversized body → `413`; rebuild on snapshot → `409`; JSON create unchanged.
- `cmd/pici-cli` (archive builder): on a temp git repo, includes modified and
  untracked non-ignored files, excludes ignored files, and omits
  `config`/`hooks/` from the embedded `.git`.
- `internal/ci`: snapshot execution selects extraction instead of clone and sets
  `PICI_SOURCE`; check runs are not attempted.
- Standard `go test ./...`; no new test framework.

## Open questions

None. Scope is fixed: snapshot upload only, no local mode, no rebuild.
