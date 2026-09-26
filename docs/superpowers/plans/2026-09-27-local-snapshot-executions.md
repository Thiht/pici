# Local Snapshot Executions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `pici-cli run --local` upload a git worktree (uncommitted changes included) and run a workflow on it server-side, without pushing to the remote.

**Architecture:** A new `Source` enum (`git` | `snapshot`) and a nullable `snapshot_id` on executions. The CLI builds a `.tar.gz` of the worktree plus `.git/` (minus `config`/`hooks`/`logs`) and POSTs it as `multipart/form-data` to the existing `POST /api/projects/{id}/executions`. The handler stores the archive under `<workspace>/uploads/<projID>/<snapshotID>.tar.gz` before enqueueing; the runner extracts it into `repoDir` instead of cloning, then recreates the git remote from `PICI_REPO_URL`. A hardened `internal/archive` package does the extraction.

**Tech Stack:** Go 1.27 (`net/http`, `archive/tar`, `compress/gzip`, std `uuid`), go-git v5, SQLite (modernc) + Postgres (pgx), goose migrations, Cobra CLI, `//go:generate go tool go-enum`.

**Spec:** `docs/superpowers/specs/2026-09-27-local-snapshot-executions-design.md`

## Global Constraints

- Go 1.27.1; no new module dependencies.
- Follow `AGENTS.md`: no tiny helper functions for trivial things; prefer direct pointer scanning over `sql.NullXxx`; type ids as `uuid.UUID` and enums as generated go-enum types.
- Do NOT add code comments unless already idiomatic in the surrounding file.
- Snapshot upload limit default: `512 << 20` bytes (512 MiB), flag `-max-snapshot-size` / env `PICI_MAX_SNAPSHOT_SIZE`.
- Archive extraction default cap when unset: `4 << 30` bytes decompressed.
- Extraction may only ever write under the execution's `repoDir`.
- The CLI never sends `.git/config`, `.git/hooks/**`, or `.git/logs/**`.
- Snapshot executions are never rebuilt (`409` / hidden button) and never create GitHub check runs.
- Every task ends with `go test ./...` green and a commit.

---

### Task 1: `Source` enum, migration and store persistence

**Files:**
- Modify: `internal/stores/enums.go`
- Create: `internal/stores/enums_enum.go` (regenerated)
- Create: `internal/stores/migrations/sqlite/004_execution_source.sql`
- Create: `internal/stores/migrations/postgres/004_execution_source.sql`
- Modify: `internal/stores/models.go`
- Modify: `internal/stores/sqlite.go`
- Modify: `internal/stores/postgres.go`
- Test: `internal/stores/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `stores.Source`, `stores.SourceGit`, `stores.SourceSnapshot`, `stores.Execution.Source stores.Source`, `stores.Execution.SnapshotID *uuid.UUID`.

- [ ] **Step 1: Add the enum**

Append to `internal/stores/enums.go`:

```go
// Source is where an execution's code comes from.
// ENUM(git, snapshot)
type Source string
```

- [ ] **Step 2: Regenerate the enum**

Run: `go generate ./internal/stores`
Expected: `internal/stores/enums_enum.go` now contains `SourceGit`, `SourceSnapshot`, `SourceNames`, `x.Value()`, `x.Scan()`.

Verify: `rg "SourceSnapshot" internal/stores/enums_enum.go`

- [ ] **Step 3: Add the migration (sqlite)**

Create `internal/stores/migrations/sqlite/004_execution_source.sql`:

```sql
-- +goose Up
ALTER TABLE executions ADD COLUMN source TEXT NOT NULL DEFAULT 'git';
ALTER TABLE executions ADD COLUMN snapshot_id TEXT;

-- +goose Down
ALTER TABLE executions DROP COLUMN snapshot_id;
ALTER TABLE executions DROP COLUMN source;
```

- [ ] **Step 4: Add the migration (postgres)**

Create `internal/stores/migrations/postgres/004_execution_source.sql`:

```sql
-- +goose Up
CREATE TYPE execution_source_enum AS ENUM ('git', 'snapshot');
ALTER TABLE executions ADD COLUMN source execution_source_enum NOT NULL DEFAULT 'git';
ALTER TABLE executions ADD COLUMN snapshot_id uuid;

-- +goose Down
ALTER TABLE executions DROP COLUMN snapshot_id;
ALTER TABLE executions DROP COLUMN source;
DROP TYPE IF EXISTS execution_source_enum;
```

- [ ] **Step 5: Add the fields to the model**

In `internal/stores/models.go`, in `type Execution struct`, after `Trigger`:

```go
	Source          Source     `json:"source"`
```

and after `ConcurrencyGroup`:

```go
	SnapshotID *uuid.UUID `json:"-"`
```

- [ ] **Step 6: Persist and read them (sqlite)**

In `internal/stores/sqlite.go` `CreateExecution`, default the source and write both columns:

```go
	if e.Source == "" {
		e.Source = SourceGit
	}
```

The std `uuid` type has no `database/sql` `Valuer`, so convert it to a string inline. Replace the INSERT with:

```go
	var snapshotID any
	if e.SnapshotID != nil {
		snapshotID = e.SnapshotID.String()
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO executions (project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, concurrency_group, source, snapshot_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, e.ProjectID.String(), e.ID, e.Workflow, e.Ref, e.CommitSHA, e.Status, e.Trigger, e.Steps, e.Error, startedMs, setupFinishedMs, finishedMs, e.CreatedAt.UnixMilli(), e.ConcurrencyGroup, e.Source, snapshotID); err != nil {
		return err
	}
```

In `GetExecution`, add `source, snapshot_id` to both the SELECT list and `Scan`, and parse the id:

```go
	var snapshotID *string
```

```go
        SELECT project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, concurrency_group, source, snapshot_id
```

```go
    `, projectID.String(), id).Scan(&projectIDStr, &e.ID, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &startedMs, &setupFinishedMs, &finishedMs, &createdMs, &e.ConcurrencyGroup, &e.Source, &snapshotID)
```

then after the `e.ProjectID = uuid.Parse(...)` block add:

```go
	if snapshotID != nil {
		parsed, err := uuid.Parse(*snapshotID)
		if err != nil {
			return Execution{}, err
		}
		e.SnapshotID = &parsed
	}
```

In `ListExecutions`, add `source, snapshot_id` to the SELECT and Scan and the same parse inside the loop:

```go
		var snapshotID *string
```

```go
        SELECT project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, concurrency_group, source, snapshot_id
```

```go
		if err := rows.Scan(&projectIDStr, &e.ID, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &startedMs, &setupFinishedMs, &finishedMs, &createdMs, &e.ConcurrencyGroup, &e.Source, &snapshotID); err != nil {
			return nil, err
		}
```

```go
		if snapshotID != nil {
			parsed, err := uuid.Parse(*snapshotID)
			if err != nil {
				return nil, err
			}
			e.SnapshotID = &parsed
		}
```

- [ ] **Step 7: Persist and read them (postgres)**

In `internal/stores/postgres.go` `CreateExecution`, add the same source default and change the INSERT:

```go
	if e.Source == "" {
		e.Source = SourceGit
	}
```

```go
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO executions (project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, concurrency_group, source, snapshot_id)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
    `, e.ProjectID.String(), e.ID, e.Workflow, e.Ref, e.CommitSHA, e.Status, e.Trigger, e.Steps, e.Error, e.StartedAt, e.SetupFinishedAt, e.FinishedAt, e.CreatedAt, e.ConcurrencyGroup, e.Source, snapshotID); err != nil {
		return err
	}
```

with, just before it:

```go
	var snapshotID any
	if e.SnapshotID != nil {
		snapshotID = e.SnapshotID.String()
	}
```

In `GetExecution` and `ListExecutions`, add `source, snapshot_id` to the SELECT and Scan. `snapshot_id` is a Postgres `uuid`; scan into `*string` then `uuid.Parse` (same pattern as sqlite):

```go
		var snapshotID *string
```

```go
        SELECT project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, concurrency_group, source, snapshot_id
```

```go
	}, projectID.String(), id).Scan(&projectIDStr, &e.ID, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &e.StartedAt, &e.SetupFinishedAt, &e.FinishedAt, &e.CreatedAt, &e.ConcurrencyGroup, &e.Source, &snapshotID)
```

plus the same `if snapshotID != nil { parsed, ... }` block (with `return Execution{}, err` / `return nil, err`) as sqlite.

- [ ] **Step 8: Write the failing test**

In `internal/stores/store_test.go`, after `TestExecutionCRUD`, add:

```go
func TestExecutionSourceRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	projectID := mustUUID("33333333-3333-3333-3333-333333333333")

	if _, err := s.CreateProject(ctx, Project{ID: projectID, Name: "demo"}); err != nil {
		t.Fatal(err)
	}

	snapshotID := mustUUID("44444444-4444-4444-4444-444444444444")
	e := Execution{
		ProjectID:  projectID,
		Workflow:   "build",
		Status:     StatusPending,
		Source:     SourceSnapshot,
		SnapshotID: &snapshotID,
	}
	if err := s.CreateExecution(ctx, &e); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetExecution(ctx, projectID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != SourceSnapshot {
		t.Fatalf("source = %q, want snapshot", got.Source)
	}
	if got.SnapshotID == nil || *got.SnapshotID != snapshotID {
		t.Fatalf("snapshot id = %v, want %s", got.SnapshotID, snapshotID)
	}

	list, err := s.ListExecutions(ctx, projectID, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(list), err)
	}
	if list[0].Source != SourceSnapshot {
		t.Fatalf("listed source = %q, want snapshot", list[0].Source)
	}
}
```

- [ ] **Step 9: Run the test to verify it fails**

Run: `go test ./internal/stores -run TestExecutionSourceRoundTrip -v`
Expected: FAIL to compile (`SourceSnapshot` undefined) before Steps 1–7; PASS after.

- [ ] **Step 10: Run the package tests**

Run: `go test ./internal/stores ./...`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/stores
git commit -m "Add execution source and snapshot id"
```

---

### Task 2: Hardened `internal/archive` extraction

**Files:**
- Create: `internal/archive/archive.go`
- Test: `internal/archive/archive_test.go`

**Interfaces:**
- Consumes: std library only.
- Produces: `archive.ExtractTarGz(src io.Reader, dest string, maxBytes int64) error`.

- [ ] **Step 1: Write the failing tests**

Create `internal/archive/archive_test.go`:

```go
package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name     string
	body     string
	typeflag byte
	link     string
	mode     int64
}

func tarGz(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body))}
		if e.typeflag != 0 {
			hdr.Typeflag = e.typeflag
			hdr.Size = 0
		}
		if e.link != "" {
			hdr.Linkname = e.link
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func extract(t *testing.T, data []byte) (string, error) {
	t.Helper()
	dest := t.TempDir()
	return dest, ExtractTarGz(bytes.NewReader(data), dest, 0)
}

func TestExtractRegularFile(t *testing.T) {
	dest, err := extract(t, tarGz(t, entry{name: "src/a.txt", body: "hello", mode: 0o644}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "src", "a.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	if _, err := extract(t, tarGz(t, entry{name: "../evil", body: "x"})); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if _, err := extract(t, tarGz(t, entry{name: "/etc/evil", body: "x"})); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
}

func TestExtractRejectsEscapingSymlink(t *testing.T) {
	data := tarGz(t, entry{name: "link", typeflag: tar.TypeSymlink, link: "../../outside"})
	if _, err := extract(t, data); err == nil {
		t.Fatal("expected escaping symlink to be rejected")
	}
}

func TestExtractAllowsInternalSymlink(t *testing.T) {
	data := tarGz(t,
		entry{name: "a.txt", body: "x"},
		entry{name: "link", typeflag: tar.TypeSymlink, link: "a.txt"},
	)
	dest, err := extract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(dest, "link"))
	if err != nil || target != "a.txt" {
		t.Fatalf("readlink = %q err=%v", target, err)
	}
}

func TestExtractRejectsSpecialFiles(t *testing.T) {
	data := tarGz(t, entry{name: "dev", typeflag: tar.TypeChar})
	if _, err := extract(t, data); err == nil {
		t.Fatal("expected char device to be rejected")
	}
}

func TestExtractEnforcesMaxBytes(t *testing.T) {
	data := tarGz(t, entry{name: "big", body: strings.Repeat("a", 1024)})
	dest := t.TempDir()
	if err := ExtractTarGz(bytes.NewReader(data), dest, 64); err == nil {
		t.Fatal("expected size limit to be enforced")
	}
}

func TestExtractStripsSetuid(t *testing.T) {
	data := tarGz(t, entry{name: "bin/tool", body: "x", mode: 0o4755})
	dest, err := extract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o4000 != 0 {
		t.Fatalf("setuid bit not stripped: %v", info.Mode())
	}
}

func TestExtractRejectsInvalidGzip(t *testing.T) {
	if _, err := extract(t, []byte("not gzip")); err == nil {
		t.Fatal("expected invalid gzip to be rejected")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/archive -v`
Expected: FAIL to compile (`ExtractTarGz` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/archive/archive.go`:

```go
package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxEntries = 1_000_000

// ExtractTarGz extracts a gzip-compressed tar stream into dest. Every entry is
// confined to dest: absolute paths, "..", escaping links and special files are
// rejected, a parent directory may not be a symlink, and the total decompressed
// size is bounded by maxBytes (4 GiB when unset).
func ExtractTarGz(src io.Reader, dest string, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 4 << 30
	}
	gz, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	lr := &io.LimitedReader{R: gz, N: maxBytes + 1}
	tr := tar.NewReader(lr)
	var entries int
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		entries++
		if entries > maxEntries {
			return errors.New("archive has too many entries")
		}
		if err := extractEntry(tr, hdr, dest); err != nil {
			return fmt.Errorf("%s: %w", hdr.Name, err)
		}
	}
	if lr.N <= 0 {
		return errors.New("archive exceeds maximum uncompressed size")
	}
	return nil
}

func extractEntry(tr *tar.Reader, hdr *tar.Header, dest string) error {
	name := filepath.FromSlash(hdr.Name)
	if name == "" || name == "." || !filepath.IsLocal(name) {
		return errors.New("invalid entry path")
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		path, err := securePath(dest, name)
		if err != nil {
			return err
		}
		fi, err := os.Lstat(path)
		if err == nil {
			if !fi.IsDir() {
				return errors.New("directory entry over a non-directory")
			}
			return nil
		}
		return os.Mkdir(path, dirMode(hdr.Mode))
	case tar.TypeReg, tar.TypeRegA:
		path, err := securePath(dest, name)
		if err != nil {
			return err
		}
		return writeFile(tr, path, fileMode(hdr.Mode))
	case tar.TypeSymlink:
		return writeSymlink(dest, name, hdr.Linkname)
	case tar.TypeLink:
		return writeHardlink(dest, name, hdr.Linkname)
	case tar.TypeXGlobalHeader, tar.TypeXHeader:
		return nil
	default:
		return fmt.Errorf("unsupported entry type %d", hdr.Typeflag)
	}
}

// securePath creates the parent directories of name (if needed) and returns the
// target path. It refuses to traverse an existing symlink or non-directory.
func securePath(dest, name string) (string, error) {
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	cur := dest
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(cur, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("invalid parent path component")
		}
	}
	return filepath.Join(cur, parts[len(parts)-1]), nil
}

func writeFile(r io.Reader, path string, mode os.FileMode) error {
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeSymlink(dest, name, link string) error {
	if filepath.IsAbs(filepath.FromSlash(link)) {
		return errors.New("absolute link target")
	}
	path, err := securePath(dest, name)
	if err != nil {
		return err
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(link)))
	if resolved != dest && !strings.HasPrefix(resolved, dest+string(filepath.Separator)) {
		return errors.New("link target escapes destination")
	}
	_ = os.Remove(path)
	return os.Symlink(link, path)
}

func writeHardlink(dest, name, link string) error {
	linkName := filepath.FromSlash(link)
	if filepath.IsAbs(linkName) || !filepath.IsLocal(linkName) {
		return errors.New("invalid link target")
	}
	target := filepath.Join(dest, linkName)
	if target != dest && !strings.HasPrefix(target, dest+string(filepath.Separator)) {
		return errors.New("link target escapes destination")
	}
	path, err := securePath(dest, name)
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Link(target, path)
}

func fileMode(m int64) os.FileMode { return os.FileMode(m).Perm() }

func dirMode(m int64) os.FileMode {
	mode := os.FileMode(m).Perm()
	if mode&0o700 == 0 {
		mode |= 0o700
	}
	return mode
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/archive -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/archive
git commit -m "Add hardened tar.gz extraction"
```

---

### Task 3: Runner materializes snapshot source

**Files:**
- Modify: `internal/ci/runner.go`
- Test: `internal/ci/runner_test.go`

**Interfaces:**
- Consumes: `stores.SourceSnapshot`, `stores.Execution.SnapshotID`, `archive.ExtractTarGz`.
- Produces: `(*Runner).EnqueueSnapshot(ctx, project, workflow, ref, commitSHA string, snapshotID uuid.UUID) (stores.Execution, error)`; `(*Runner).MaxSnapshotSize int64`; `PICI_SOURCE` env var.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ci/runner_test.go`:

```go
func TestBuildEnvSnapshotSource(t *testing.T) {
	env := buildEnv(stores.Project{}, stores.Execution{Source: stores.SourceSnapshot}, "/workspace", "abcdef12345", "abcdef1")
	if !slices.Contains(env, "PICI_SOURCE=snapshot") {
		t.Fatalf("missing PICI_SOURCE=snapshot in %v", env)
	}
}

func TestMaterializeSnapshot(t *testing.T) {
	projectID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	snapshotID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	ws := t.TempDir()
	r := &Runner{WorkspaceDir: ws, MaxSnapshotSize: 1 << 20}

	dir := filepath.Join(ws, "uploads", projectID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, filepath.Join(dir, snapshotID.String()+".tar.gz"), map[string]string{
		".git/HEAD":          "ref: refs/heads/main\n",
		".ci/build/ci.yml":   "steps:\n",
	})

	repoDir := filepath.Join(ws, projectID.String(), "1")
	project := stores.Project{ID: projectID, RepoURL: "https://example.com/acme/demo.git"}
	exec := stores.Execution{Source: stores.SourceSnapshot, SnapshotID: &snapshotID}
	if err := r.materializeSource(context.Background(), project, exec, repoDir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(repoDir, ".ci", "build", "ci.yml")); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(filepath.Join(repoDir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), project.RepoURL) {
		t.Fatalf("config missing origin url: %s", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, snapshotID.String()+".tar.gz")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected archive to be removed after extraction")
	}
}

func writeSnapshot(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}
```

Update the test imports at the top of `internal/ci/runner_test.go`:

```go
import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/Thiht/pici/internal/stores"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ci -run 'TestBuildEnvSnapshotSource|TestMaterializeSnapshot' -v`
Expected: FAIL (`materializeSource` undefined, `stores` unused import ok once used).

- [ ] **Step 3: Add `MaxSnapshotSize` and `EnqueueSnapshot`**

In `internal/ci/runner.go`, add a field to `Runner` after `DefaultStepTimeout`:

```go
	MaxSnapshotSize    int64
```

Replace the body of `Enqueue` and add `EnqueueSnapshot` + helpers:

```go
func (r *Runner) Enqueue(ctx context.Context, project stores.Project, workflow, ref, commitSHA string, trigger stores.Trigger) (stores.Execution, error) {
	return r.enqueue(ctx, stores.Execution{
		ProjectID: project.ID,
		Project:   project.Name,
		Workflow:  workflow,
		Ref:       ref,
		CommitSHA: commitSHA,
		Trigger:   trigger,
		Source:    stores.SourceGit,
		CreatedAt: time.Now(),
	})
}

// EnqueueSnapshot queues an execution whose code comes from a locally uploaded
// archive instead of a git clone.
func (r *Runner) EnqueueSnapshot(ctx context.Context, project stores.Project, workflow, ref, commitSHA string, snapshotID uuid.UUID) (stores.Execution, error) {
	return r.enqueue(ctx, stores.Execution{
		ProjectID:  project.ID,
		Project:    project.Name,
		Workflow:   workflow,
		Ref:        ref,
		CommitSHA:  commitSHA,
		Trigger:    stores.TriggerManual,
		Source:     stores.SourceSnapshot,
		SnapshotID: &snapshotID,
		CreatedAt:  time.Now(),
	})
}

func (r *Runner) enqueue(ctx context.Context, exec stores.Execution) (stores.Execution, error) {
	if exec.Status == "" {
		exec.Status = stores.StatusPending
	}
	if err := r.Store.CreateExecution(ctx, &exec); err != nil {
		return stores.Execution{}, err
	}
	return exec, nil
}
```

- [ ] **Step 4: Materialize the source in `run()`**

In `run()`, replace the clone block:

```go
	repoDir := filepath.Join(r.WorkspaceDir, project.ID.String(), strconv.FormatInt(exec.ID, 10))
	cloneRef := exec.Ref
	if exec.CommitSHA != "" {
		cloneRef = exec.CommitSHA
	}
	if err := git.Clone(ctx, projectCloneConfig(project, repoDir, cloneRef)); err != nil {
		fmt.Fprintf(setupLog, "clone failed: %v\n", err)
		r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), project, setupLog, 0)
		return
	}
```

with:

```go
	repoDir := filepath.Join(r.WorkspaceDir, project.ID.String(), strconv.FormatInt(exec.ID, 10))
	if err := r.materializeSource(ctx, project, exec, repoDir); err != nil {
		fmt.Fprintf(setupLog, "prepare source failed: %v\n", err)
		r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), project, setupLog, 0)
		return
	}
```

Add these methods after `run()`:

```go
func (r *Runner) materializeSource(ctx context.Context, project stores.Project, exec stores.Execution, repoDir string) error {
	if exec.Source != stores.SourceSnapshot {
		cloneRef := exec.Ref
		if exec.CommitSHA != "" {
			cloneRef = exec.CommitSHA
		}
		return git.Clone(ctx, projectCloneConfig(project, repoDir, cloneRef))
	}
	if exec.SnapshotID == nil {
		return errors.New("snapshot execution is missing its snapshot id")
	}
	src := r.snapshotPath(project.ID, *exec.SnapshotID)
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := archive.ExtractTarGz(f, repoDir, r.MaxSnapshotSize); err != nil {
		return err
	}
	_ = os.Remove(src)

	// .git/config is excluded from the upload; recreate just the origin remote
	// from the project's own repo URL.
	configPath := filepath.Join(repoDir, ".git", "config")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	config := "[remote \"origin\"]\n\turl = " + project.RepoURL + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	return os.WriteFile(configPath, []byte(config), 0o644)
}

func (r *Runner) snapshotPath(projectID, snapshotID uuid.UUID) string {
	return filepath.Join(r.WorkspaceDir, "uploads", projectID.String(), snapshotID.String()+".tar.gz")
}
```

Add the `archive` import to `internal/ci/runner.go`:

```go
	"github.com/Thiht/pici/internal/archive"
```

- [ ] **Step 5: Skip check runs and schedules for snapshots**

At the top of `createCheckRun`, add:

```go
	if exec.Source == stores.SourceSnapshot {
		return 0
	}
```

In `run()`, wrap the schedule sync:

```go
	if exec.Source != stores.SourceSnapshot {
		if err := SyncSchedule(ctx, r.Store, project.ID, exec.Workflow, cfg.Schedule); err != nil {
			fmt.Fprintf(setupLog, "schedule sync failed: %v\n", err)
		}
	}
```

- [ ] **Step 6: Expose `PICI_SOURCE`**

In `buildEnv`, add before the `return []string{`:

```go
	source := exec.Source
	if source == "" {
		source = stores.SourceGit
	}
```

and add this line inside the returned slice:

```go
		"PICI_SOURCE=" + string(source),
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/ci -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ci
git commit -m "Materialize snapshot executions in the runner"
```

---

### Task 4: Multipart upload endpoint and config limit

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cmd/pici/main.go`
- Modify: `internal/handlers/handlers.go`
- Modify: `internal/handlers/executions.go`
- Test: `internal/handlers/executions_test.go` (create)
- Test: `internal/handlers/auth_test.go`, `internal/handlers/webhooks_test.go` (update `New` calls)

**Interfaces:**
- Consumes: `(*Runner).EnqueueSnapshot`, `stores.SourceSnapshot`.
- Produces: `handlers.New(store, runner, engine, workspaceDir, mountPath string, maxSnapshotSize int64, apiToken, buildVersion string) *Handler`; `config.Config.MaxSnapshotSize int64`.

- [ ] **Step 1: Add the config field**

In `internal/config/config.go`, add to `Config`:

```go
	MaxSnapshotSize int64
```

and register the flag in `Load` (after `StepTimeout`):

```go
	fs.Int64Var(&cfg.MaxSnapshotSize, "max-snapshot-size", 512<<20, "maximum size in bytes of an uploaded local snapshot")
```

- [ ] **Step 2: Pass it through main**

In `cmd/pici/main.go`, set the runner field:

```go
		MaxSnapshotSize:    cfg.MaxSnapshotSize,
```

and update the API handler construction:

```go
	apiHandler := handlers.New(store, runner, engine, cfg.WorkspaceDir, cfg.RepoMountPath, cfg.MaxSnapshotSize, cfg.APIToken, buildVersion).Routes()
```

- [ ] **Step 3: Update handler constructors**

In `internal/handlers/handlers.go`:

```go
func New(store stores.Store, runner *ci.Runner, engine *docker.Engine, workspaceDir, mountPath string, maxSnapshotSize int64, apiToken, buildVersion string) *Handler {
	return &Handler{
		projects:   NewProjectsHandler(store, runner, workspaceDir),
		variables:  NewVariablesHandler(store),
		executions: NewExecutionsHandler(store, runner, workspaceDir, maxSnapshotSize),
		webhooks:   NewWebhooksHandler(store, runner, workspaceDir),
		artifacts:  NewArtifactsHandler(store, runner),
		system:     NewSystemHandler(store, engine, buildVersion),
		cache:      NewCacheHandler(store, engine),
		apiToken:   apiToken,
	}
}
```

In `internal/handlers/executions.go`, change the struct and constructor:

```go
type ExecutionsHandler struct {
	store           stores.Store
	runner          *ci.Runner
	workspaceDir    string
	maxSnapshotSize int64
}

func NewExecutionsHandler(store stores.Store, runner *ci.Runner, workspaceDir string, maxSnapshotSize int64) *ExecutionsHandler {
	return &ExecutionsHandler{store: store, runner: runner, workspaceDir: workspaceDir, maxSnapshotSize: maxSnapshotSize}
}
```

- [ ] **Step 4: Update existing tests' `New` calls**

In `internal/handlers/auth_test.go`, change:

```go
	return New(store, &ci.Runner{Store: store}, nil, t.TempDir(), "/workspace", token, "dev")
```

to:

```go
	return New(store, &ci.Runner{Store: store}, nil, t.TempDir(), "/workspace", 512<<20, token, "dev")
```

In `internal/handlers/webhooks_test.go`, change:

```go
	s := New(store, runner, nil, t.TempDir(), "/workspace", "", "dev")
```

to:

```go
	s := New(store, runner, nil, t.TempDir(), "/workspace", 512<<20, "", "dev")
```

- [ ] **Step 5: Write the failing tests**

Create `internal/handlers/executions_test.go`:

```go
package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func newExecServer(t *testing.T, maxSnapshotSize int64) (*Handler, stores.Store, string, uuid.UUID) {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	projectID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	if _, err := store.CreateProject(t.Context(), stores.Project{
		ID:            projectID,
		Name:          "demo",
		RepoURL:       "https://example.com/acme/demo.git",
		DefaultBranch: "main",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	s := New(store, &ci.Runner{Store: store}, nil, ws, "/workspace", maxSnapshotSize, "", "dev")
	return s, store, ws, projectID
}

func snapshotRequest(t *testing.T, projectID uuid.UUID, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("workflow", "build")
	_ = mw.WriteField("ref", "main")
	part, err := mw.CreateFormFile("snapshot", "snapshot.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID.String()+"/executions", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestCreateSnapshotExecution(t *testing.T) {
	s, store, ws, projectID := newExecServer(t, 1<<20)

	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, snapshotRequest(t, projectID, []byte("fake archive")))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	execs, err := store.ListExecutions(t.Context(), projectID, 10)
	if err != nil || len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(execs), err)
	}
	if execs[0].Source != stores.SourceSnapshot || execs[0].SnapshotID == nil {
		t.Fatalf("unexpected execution: %+v", execs[0])
	}
	path := filepath.Join(ws, "uploads", projectID.String(), execs[0].SnapshotID.String()+".tar.gz")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("archive not stored: %v", err)
	}
}

func TestCreateSnapshotTooLarge(t *testing.T) {
	s, _, projectID := newExecServer(t, 8)

	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, snapshotRequest(t, projectID, bytes.Repeat([]byte("a"), 4096)))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRebuildSnapshotRejected(t *testing.T) {
	s, store, _, projectID := newExecServer(t, 1<<20)

	snapshotID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	e := stores.Execution{ProjectID: projectID, Workflow: "build", Status: stores.StatusSuccess, Source: stores.SourceSnapshot, SnapshotID: &snapshotID}
	if err := store.CreateExecution(t.Context(), &e); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID.String()+"/executions/"+itoa(e.ID)+"/rebuild", nil)
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
```

Add `"strconv"` to that file's imports.

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./internal/handlers -run 'Snapshot|Rebuild' -v`
Expected: FAIL (multipart falls through to JSON parsing / `409` missing).

- [ ] **Step 7: Branch on content type and implement the upload**

In `internal/handlers/executions.go`, replace the beginning of `Create` after the project resolution:

```go
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
		h.createSnapshot(w, r, project)
		return
	}
```

Add the new method and update `Rebuild`:

```go
func (h *ExecutionsHandler) createSnapshot(w http.ResponseWriter, r *http.Request, project stores.Project) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxSnapshotSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			render.Error(w, http.StatusRequestEntityTooLarge, errors.New("snapshot exceeds maximum size"))
			return
		}
		render.Error(w, http.StatusBadRequest, err)
		return
	}

	workflow := strings.TrimSpace(r.FormValue("workflow"))
	if workflow == "" {
		render.Error(w, http.StatusBadRequest, errors.New("workflow is required"))
		return
	}
	ref := r.FormValue("ref")
	if ref == "" {
		ref = project.DefaultBranch
	}

	file, _, err := r.FormFile("snapshot")
	if err != nil {
		render.Error(w, http.StatusBadRequest, errors.New("snapshot file is required"))
		return
	}
	defer file.Close()

	snapshotID := uuid.New()
	dir := filepath.Join(h.workspaceDir, "uploads", project.ID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	tmp := filepath.Join(dir, snapshotID.String()+".tar.gz.tmp")
	dst := filepath.Join(dir, snapshotID.String()+".tar.gz")
	out, err := os.Create(tmp)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		render.Error(w, http.StatusInternalServerError, err)
		return
	}

	execution, err := h.runner.EnqueueSnapshot(r.Context(), project, workflow, ref, r.FormValue("commit_sha"), snapshotID)
	if err != nil {
		_ = os.Remove(dst)
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusAccepted, execution)
}
```

In `Rebuild`, after fetching `previous`:

```go
	if previous.Source == stores.SourceSnapshot {
		render.Error(w, http.StatusConflict, errors.New("snapshot executions cannot be rebuilt"))
		return
	}
```

Add imports to `internal/handlers/executions.go`: `"mime"`, `"uuid"` (the others — `errors`, `io`, `os`, `filepath`, `strings` — are already imported).

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/handlers -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/config internal/handlers cmd/pici/main.go
git commit -m "Accept snapshot uploads on the executions endpoint"
```

---

### Task 5: CLI `run --local` and archive builder

**Files:**
- Modify: `client/client.go`
- Create: `client/snapshot.go`
- Modify: `cmd/pici-cli/main.go`
- Create: `cmd/pici-cli/local.go`
- Test: `cmd/pici-cli/local_test.go`

**Interfaces:**
- Consumes: `POST /api/projects/{id}/executions` multipart.
- Produces: `(*client.Client).TriggerSnapshotExecution(ctx, project, workflow, ref, commitSHA string, snapshot io.Reader) (Execution, error)`; `buildSnapshot(dir string, w io.Writer) error`; `worktreeInfo(dir string) (ref, sha string, err error)`.

- [ ] **Step 1: Add a no-timeout upload client**

In `client/client.go`, add a field to `Client`:

```go
	upload *http.Client
```

In `New`:

```go
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
		upload:  &http.Client{},
	}
```

Refactor `send` to delegate:

```go
func (c *Client) send(req *http.Request) ([]byte, error) {
	return c.sendWith(c.http, req)
}

func (c *Client) sendWith(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, &httpError{status: resp.Status, body: strings.TrimSpace(string(data))}
	}
	return data, nil
}
```

- [ ] **Step 2: Add the client call**

Create `client/snapshot.go`:

```go
package client

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
)

// TriggerSnapshotExecution uploads a tar.gz of a local worktree and runs the
// workflow on it, without pushing to the project's git remote.
func (c *Client) TriggerSnapshotExecution(ctx context.Context, project, workflow, ref, commitSHA string, snapshot io.Reader) (Execution, error) {
	var out Execution
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType()

	go func() {
		var err error
		if workflow != "" {
			err = mw.WriteField("workflow", workflow)
		}
		if err == nil && ref != "" {
			err = mw.WriteField("ref", ref)
		}
		if err == nil && commitSHA != "" {
			err = mw.WriteField("commit_sha", commitSHA)
		}
		var part io.Writer
		if err == nil {
			part, err = mw.CreateFormFile("snapshot", "snapshot.tar.gz")
		}
		if err == nil {
			_, err = io.Copy(part, snapshot)
		}
		if cerr := mw.Close(); err == nil {
			err = cerr
		}
		pw.CloseWithError(err)
	}()

	req, err := c.newRequest(ctx, http.MethodPost, "/api/projects/"+project+"/executions", pr)
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", contentType)
	data, err := c.sendWith(c.upload, req)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(data, &out)
}
```

- [ ] **Step 3: Write the failing CLI test**

Create `cmd/pici-cli/local_test.go`:

```go
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestBuildSnapshot(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "t")

	mustWrite(t, filepath.Join(dir, "tracked.txt"), "v1")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")

	mustWrite(t, filepath.Join(dir, "tracked.txt"), "v2")
	mustWrite(t, filepath.Join(dir, "untracked.txt"), "new")
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "no")

	var buf bytes.Buffer
	if err := buildSnapshot(dir, &buf); err != nil {
		t.Fatal(err)
	}
	names, contents := readTarGz(t, buf.Bytes())

	if _, ok := names["tracked.txt"]; !ok {
		t.Fatal("tracked.txt missing")
	}
	if _, ok := names["untracked.txt"]; !ok {
		t.Fatal("untracked.txt missing")
	}
	if _, ok := names["ignored.txt"]; ok {
		t.Fatal("ignored.txt should be excluded")
	}
	if contents["tracked.txt"] != "v2" {
		t.Fatalf("tracked.txt content = %q, want v2", contents["tracked.txt"])
	}
	if _, ok := names[".git/HEAD"]; !ok {
		t.Fatal(".git/HEAD missing")
	}
	if _, ok := names[".git/config"]; ok {
		t.Fatal(".git/config should be excluded")
	}
}

func TestWorktreeInfo(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "t")
	mustWrite(t, filepath.Join(dir, "a"), "a")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")

	ref, sha, err := worktreeInfo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "main" {
		t.Fatalf("ref = %q, want main", ref)
	}
	if len(sha) != 40 {
		t.Fatalf("sha = %q", sha)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTarGz(t *testing.T, data []byte) (map[string]bool, map[string]string) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	contents := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[hdr.Name] = true
		if hdr.Typeflag == tar.TypeReg {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			contents[hdr.Name] = string(b)
		}
	}
	return names, contents
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./cmd/pici-cli -run 'TestBuildSnapshot|TestWorktreeInfo' -v`
Expected: FAIL (`buildSnapshot` / `worktreeInfo` undefined).

- [ ] **Step 5: Implement the builder**

Create `cmd/pici-cli/local.go`:

```go
package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func worktreeInfo(dir string) (ref, sha string, err error) {
	out, err := runGit(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("%s is not a git worktree: %w", dir, err)
	}
	sha = strings.TrimSpace(out)
	if branch, err := runGit(dir, "symbolic-ref", "--short", "HEAD"); err == nil {
		if ref = strings.TrimSpace(branch); ref == "" && len(sha) >= 7 {
			ref = sha[:7]
		}
	} else if len(sha) >= 7 {
		ref = sha[:7]
	}
	return ref, sha, nil
}

func buildSnapshot(dir string, w io.Writer) error {
	out, err := runGit(dir, "ls-files", "-c", "-o", "--exclude-standard", "-z")
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		if err := addWorktreeFile(tw, dir, rel); err != nil {
			return err
		}
	}
	if err := addGitDir(tw, dir); err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func addWorktreeFile(tw *tar.Writer, dir, rel string) error {
	full := filepath.Join(dir, filepath.FromSlash(rel))
	fi, err := os.Lstat(full)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(full)
		if err != nil {
			return err
		}
		return tw.WriteHeader(&tar.Header{Name: rel, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777})
	case fi.Mode().IsRegular():
		f, err := os.Open(full)
		if err != nil {
			return err
		}
		defer f.Close()
		hdr := &tar.Header{Name: rel, Typeflag: tar.TypeReg, Mode: int64(fi.Mode().Perm()), Size: fi.Size(), ModTime: fi.ModTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	default:
		return nil
	}
}

func addGitDir(tw *tar.Writer, dir string) error {
	root := filepath.Join(dir, ".git")
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" {
			return nil
		}
		excluded := rel == ".git/config" ||
			rel == ".git/hooks" || strings.HasPrefix(rel, ".git/hooks/") ||
			rel == ".git/logs" || strings.HasPrefix(rel, ".git/logs/")
		if excluded {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: rel + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		}
		return addWorktreeFile(tw, dir, rel)
	})
}
```

- [ ] **Step 6: Wire the flags into `run`**

In `cmd/pici-cli/main.go`, replace `newRunCmd` with:

```go
func newRunCmd(c *client.Client) *cobra.Command {
	var ref string
	var local bool
	var dir string
	cmd := &cobra.Command{
		Use:   "run <project> <workflow>",
		Short: "Trigger a workflow execution",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !local {
				exec, err := c.TriggerExecution(context.Background(), args[0], args[1], ref)
				if err != nil {
					return err
				}
				fmt.Println(exec.ID)
				return nil
			}

			workdir := dir
			if workdir == "" {
				if workdir, err = os.Getwd(); err != nil {
					return err
				}
			}
			localRef, sha, err := worktreeInfo(workdir)
			if err != nil {
				return err
			}
			if ref != "" {
				localRef = ref
			}

			pr, pw := io.Pipe()
			go func() {
				err := buildSnapshot(workdir, pw)
				pw.CloseWithError(err)
			}()

			exec, err := c.TriggerSnapshotExecution(context.Background(), args[0], args[1], localRef, sha, pr)
			if err != nil {
				return err
			}
			fmt.Println(exec.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "git ref to build")
	cmd.Flags().BoolVar(&local, "local", false, "run on the current worktree, including uncommitted changes")
	cmd.Flags().StringVar(&dir, "dir", "", "worktree directory to snapshot (default: current directory)")
	cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return projectNames(c), cobra.ShellCompDirectiveNoFileComp
		}
		workflows, err := c.ListWorkflows(context.Background(), args[0])
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return workflows, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}
```

Add `"io"` to the imports of `cmd/pici-cli/main.go`, and update the usage text line:

```go
  pici-cli run <project> <workflow> [--ref <ref>] [--local [--dir <path>]]
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./cmd/pici-cli -v && go build ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add client cmd/pici-cli
git commit -m "Add pici-cli run --local snapshot upload"
```

---

### Task 6: GC cleanup, UI badge, rebuild guard, docs

**Files:**
- Modify: `internal/gc/gc.go`
- Modify: `internal/web/templates/execution.html`
- Modify: `internal/web/templates/project_show.html`
- Modify: `internal/web/executions.go`
- Modify: `README.md`
- Test: `internal/gc/gc_test.go` (create if absent)

**Interfaces:**
- Consumes: `stores.SourceSnapshot`, `<workspace>/uploads`.
- Produces: nothing consumed elsewhere.

- [ ] **Step 1: Write the failing GC test**

Create `internal/gc/gc_test.go`:

```go
package gc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupUploads(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "uploads", "11111111-1111-1111-1111-111111111111")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "old.tar.gz")
	stale := filepath.Join(dir, "stale.tar.gz")
	fresh := filepath.Join(dir, "fresh.tar.gz")
	for _, p := range []string{old, stale, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}

	c := &Collector{WorkspaceDir: ws, LogsDir: filepath.Join(ws, "logs"), Keep: time.Hour}
	c.cleanupUploads()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expected old upload to be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("expected fresh upload to be kept")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/gc -run TestCleanupUploads -v`
Expected: FAIL (`cleanupUploads` undefined).

- [ ] **Step 3: Implement the cleanup**

In `internal/gc/gc.go`, call it from `tick`:

```go
func (c *Collector) tick(ctx context.Context) {
	c.cleanupWorkspaces(ctx)
	c.cleanupLogs(ctx)
	c.cleanupUploads()
	if c.Engine != nil {
		if err := c.Engine.PruneImages(ctx); err != nil {
			slog.WarnContext(ctx, "prune images", "error", err)
		}
	}
}
```

Add the method after `cleanupLogs`:

```go
func (c *Collector) cleanupUploads() {
	root := filepath.Join(c.WorkspaceDir, "uploads")
	projects, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, proj := range projects {
		if !proj.IsDir() {
			continue
		}
		projPath := filepath.Join(root, proj.Name())
		files, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			path := filepath.Join(projPath, f.Name())
			if olderThan(path, c.Keep) {
				_ = os.RemoveAll(path)
			}
		}
	}
}
```

- [ ] **Step 4: Guard rebuild in the web UI**

In `internal/web/executions.go` `ExecutionRebuild`, after fetching `previous`:

```go
	if previous.Source == stores.SourceSnapshot {
		setFlash(w, "error", "Snapshot executions cannot be rebuilt.")
		redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(id, 10))
		return
	}
```

- [ ] **Step 5: Add the badge and hide rebuild (detail page)**

In `internal/web/templates/execution.html`, in the title block:

```html
<h1 class="text-xl font-semibold">{{.Execution.Workflow}} <span class="opacity-50">#{{.Execution.ID}}</span></h1>
<span class="{{statusClass .Execution.Status}}">{{capitalize .Execution.Status}}</span>
{{if eq (printf "%s" .Execution.Source) "snapshot"}}<span class="badge badge-soft badge-accent">local</span>{{end}}
```

Wrap the rebuild form:

```html
{{if ne (printf "%s" .Execution.Source) "snapshot"}}
<form method="post" action="/projects/{{.Project.ID}}/executions/{{.Execution.ID}}/rebuild">
<input type="hidden" name="_csrf" value="{{.CSRFToken}}">
<button class="btn btn-outline btn-sm" type="submit">Rebuild</button>
</form>
{{end}}
```

- [ ] **Step 6: Add the badge to the list**

In `internal/web/templates/project_show.html`, change the workflow cell:

```html
<td><a class="link link-primary font-medium stretched-link" href="/projects/{{$.Project.ID}}/executions/{{.ID}}">{{.Workflow}}</a>{{if eq (printf "%s" .Source) "snapshot"}} <span class="badge badge-soft badge-accent">local</span>{{end}}</td>
```

- [ ] **Step 7: Update the README**

In `README.md`, add `PICI_SOURCE` to the built-in env table:

```markdown
| `PICI_SOURCE`       | `git` or `snapshot`                            |
```

And add a short section after "How a workflow runs":

````markdown
### Running on local changes

Upload the current worktree — uncommitted and untracked (non-ignored) files
included — and run a workflow on it without pushing:

```sh
pici-cli run demo build --local
```

The CLI archives the worktree plus `.git/` (never `.git/config`, which may
contain credentials) and the server extracts it instead of cloning. Snapshot
executions are marked `local`, cannot be rebuilt, and do not post check runs.
````

- [ ] **Step 8: Run the full suite**

Run: `go test ./... && go build ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/gc internal/web README.md
git commit -m "Clean up snapshot uploads and mark local executions in the UI"
```

---

## Self-Review

- **Spec coverage:** Source/enum/migration (Task 1), secure extraction (Task 2), runner materialization + env + check-run/schedule skips (Task 3), multipart API + size limit + rebuild 409 (Task 4), CLI `--local` + worktree archive minus config/hooks/logs (Task 5), GC/UI/docs (Task 6). All spec sections mapped.
- **Placeholder scan:** none — every code and test step is concrete.
- **Type consistency:** `Source`/`SourceGit`/`SourceSnapshot`, `SnapshotID *uuid.UUID`, `EnqueueSnapshot(..., snapshotID uuid.UUID)`, `ExtractTarGz(io.Reader, string, int64)`, `handlers.New(..., maxSnapshotSize int64, ...)`, `TriggerSnapshotExecution(..., io.Reader)`, `buildSnapshot(dir, io.Writer)`, `worktreeInfo(dir)` are used consistently across tasks.
