package workspace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"uuid"
)

// Workspace resolves paths inside the two roots pici writes to: one for
// workspaces, snapshots and artifacts, one for logs. They are separate because
// logs are often kept on a cheaper volume than the workspaces.
type Workspace struct {
	root     string
	logsRoot string
}

func New(root, logsRoot string) *Workspace {
	return &Workspace{root: root, logsRoot: logsRoot}
}

// RepoDir is the directory an Execution's source is materialized in. Retries
// share it through Execution.WorkspaceID.
func (w *Workspace) RepoDir(projectID uuid.UUID, workspaceID int64) string {
	return filepath.Join(w.root, projectID.String(), strconv.FormatInt(workspaceID, 10))
}

func (w *Workspace) DiscoveryDir(projectID uuid.UUID) string {
	return filepath.Join(w.root, projectID.String(), "_discovery")
}

func (w *Workspace) UploadsDir(projectID uuid.UUID) string {
	return filepath.Join(w.root, "uploads", projectID.String())
}

// Snapshot is the archive an Execution with SourceSnapshot is materialized
// from. It is deleted once the Execution is terminal.
func (w *Workspace) Snapshot(projectID, snapshotID uuid.UUID) string {
	return filepath.Join(w.UploadsDir(projectID), snapshotID.String()+".tar.gz")
}

func (w *Workspace) ArtifactDir(projectID uuid.UUID, execID int64) string {
	return filepath.Join(w.root, "artifacts", projectID.String(), strconv.FormatInt(execID, 10))
}

func (w *Workspace) StepArtifacts(projectID uuid.UUID, execID int64, index int) string {
	return filepath.Join(w.ArtifactDir(projectID, execID), fmt.Sprintf("%03d", index))
}

func (w *Workspace) StepManifest(projectID uuid.UUID, execID int64, index int) string {
	return filepath.Join(w.ArtifactDir(projectID, execID), fmt.Sprintf("%03d.json", index))
}

func (w *Workspace) LogDir(projectID uuid.UUID, execID int64) string {
	return filepath.Join(w.logsRoot, projectID.String(), strconv.FormatInt(execID, 10))
}

func (w *Workspace) SetupLog(projectID uuid.UUID, execID int64) string {
	return filepath.Join(w.LogDir(projectID, execID), "setup.log")
}

func (w *Workspace) StepLog(projectID uuid.UUID, execID int64, index int, step string) string {
	return filepath.Join(w.LogDir(projectID, execID), "steps", fmt.Sprintf("%03d_%s.log", index, Source(step)))
}

// LogPaths lists the setup log followed by every Step log, in Step order.
func (w *Workspace) LogPaths(projectID uuid.UUID, execID int64) []string {
	var out []string
	setup := w.SetupLog(projectID, execID)
	if _, err := os.Stat(setup); err == nil {
		out = append(out, setup)
	}
	stepsDir := filepath.Join(w.LogDir(projectID, execID), "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		return out
	}
	var steps []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			steps = append(steps, filepath.Join(stepsDir, e.Name()))
		}
	}
	sort.Strings(steps)
	return append(out, steps...)
}

// ReadLogs opens every log of an Execution as one stream. Logs that do not
// exist yet are skipped, so a running Execution reads as whatever it has
// produced so far.
func (w *Workspace) ReadLogs(projectID uuid.UUID, execID int64) io.ReadCloser {
	var opened []*os.File
	var readers []io.Reader
	for _, path := range w.LogPaths(projectID, execID) {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		opened = append(opened, f)
		readers = append(readers, f)
	}
	return &readCloser{Reader: io.MultiReader(readers...), files: opened}
}

func (w *Workspace) ReadSetupLog(projectID uuid.UUID, execID int64) (io.ReadCloser, error) {
	return os.Open(w.SetupLog(projectID, execID))
}

func (w *Workspace) ReadStepLog(projectID uuid.UUID, execID int64, index int, step string) (io.ReadCloser, error) {
	return os.Open(w.StepLog(projectID, execID, index, step))
}

// Source is the name a Step's log lines are streamed under. It is also the
// stem of the Step's log file, so names that are not filesystem-safe collapse
// to one spelling everywhere.
func Source(step string) string {
	var b strings.Builder
	for _, r := range step {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// LogSource is the stream source a log path belongs to: "setup", or the Step's
// Source. It is the inverse of the file naming in StepLog.
func LogSource(path string) string {
	base := filepath.Base(path)
	if base == "setup.log" {
		return "setup"
	}
	name := strings.TrimSuffix(base, ".log")
	if idx := strings.IndexByte(name, '_'); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

type readCloser struct {
	io.Reader
	files []*os.File
}

func (r *readCloser) Close() error {
	var err error
	for _, f := range r.files {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}
