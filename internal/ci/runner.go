package ci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"uuid"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/Thiht/pici/internal/archive"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/github"
	"github.com/Thiht/pici/internal/mask"
	"github.com/Thiht/pici/internal/notify"
	"github.com/Thiht/pici/internal/present"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

const maxParallelSteps = 8

var (
	ErrNotRetryable  = errors.New("execution is not retryable")
	ErrWorkspaceGone = errors.New("workspace no longer available")
)

type execKey struct {
	projectID uuid.UUID
	id        int64
}

type Engine interface {
	Build(ctx context.Context, opts docker.BuildOptions) error
	Run(ctx context.Context, opts docker.RunOptions) (int, error)
	Host() string
}

type Runner struct {
	Store              stores.Store
	Engine             Engine
	Workspace          *workspace.Workspace
	MountPath          string
	DefaultStepTimeout time.Duration
	MaxSnapshotSize    int64
	PublicBaseURL      string

	workerID string
	mu       sync.Mutex
	cancels  map[execKey]context.CancelFunc
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func (r *Runner) Start(workers int) {
	if r.workerID == "" {
		r.workerID = uuid.New().String()
	}
	r.mu.Lock()
	if r.cancels == nil {
		r.cancels = make(map[execKey]context.CancelFunc)
	}
	if r.stopCh == nil {
		r.stopCh = make(chan struct{})
	}
	r.mu.Unlock()

	for range workers {
		go r.workerLoop()
	}
}

func (r *Runner) workerLoop() {
	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		exec, err := r.Store.ClaimPendingExecution(context.Background(), r.workerID)
		if errors.Is(err, stores.ErrNotFound) {
			select {
			case <-r.stopCh:
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		key := execKey{exec.ProjectID, exec.ID}
		r.wg.Add(1)
		runCtx, cancel := context.WithCancel(context.Background())
		r.registerCancel(key, cancel)
		r.run(runCtx, exec)
		cancel()
		r.unregisterCancel(key)
		r.wg.Done()
	}
}

func (r *Runner) Drain(ctx context.Context) {
	r.stopOnce.Do(func() { close(r.stopCh) })

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		r.cancelAll()
		<-done
	}
}

func (r *Runner) cancelAll() {
	r.mu.Lock()
	for _, cancel := range r.cancels {
		cancel()
	}
	r.mu.Unlock()
}

func (r *Runner) registerCancel(key execKey, cancel context.CancelFunc) {
	r.mu.Lock()
	r.cancels[key] = cancel
	r.mu.Unlock()
}

func (r *Runner) unregisterCancel(key execKey) {
	r.mu.Lock()
	delete(r.cancels, key)
	r.mu.Unlock()
}

func (r *Runner) Cancel(projectID uuid.UUID, execID int64) {
	r.mu.Lock()
	cancel := r.cancels[execKey{projectID, execID}]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Runner) Enqueue(ctx context.Context, project stores.Project, workflow, ref, commitSHA string, trigger stores.Trigger, createdBy *uuid.UUID) (stores.Execution, error) {
	return r.enqueue(ctx, stores.Execution{
		ProjectID: project.ID,
		Project:   project.Name,
		Workflow:  workflow,
		Ref:       ref,
		CommitSHA: commitSHA,
		Trigger:   trigger,
		Source:    stores.SourceGit,
		CreatedAt: time.Now(),
		CreatedBy: createdBy,
	})
}

func (r *Runner) EnqueueSnapshot(ctx context.Context, project stores.Project, workflow, ref, commitSHA string, snapshotID uuid.UUID, createdBy *uuid.UUID) (stores.Execution, error) {
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
		CreatedBy:  createdBy,
	})
}

func (r *Runner) EnqueueRebuild(ctx context.Context, project stores.Project, previous stores.Execution, createdBy *uuid.UUID) (stores.Execution, error) {
	parentID := previous.ExecutionID
	return r.enqueue(ctx, stores.Execution{
		ProjectID: project.ID,
		Project:   project.Name,
		Workflow:  previous.Workflow,
		Ref:       previous.Ref,
		CommitSHA: previous.CommitSHA,
		Trigger:   stores.TriggerRebuild,
		Source:    stores.SourceGit,
		ParentID:  &parentID,
		CreatedAt: time.Now(),
		CreatedBy: createdBy,
	})
}

func (r *Runner) EnqueueRetry(ctx context.Context, project stores.Project, previous stores.Execution, createdBy *uuid.UUID) (stores.Execution, error) {
	latest, err := r.Store.GetExecutionAttempt(ctx, project.ID, previous.ExecutionID, 0)
	if err != nil {
		return stores.Execution{}, err
	}
	if latest.ID != previous.ID {
		return stores.Execution{}, fmt.Errorf("%w: only the latest attempt can be retried", ErrNotRetryable)
	}
	previous = latest

	if previous.Source != stores.SourceGit {
		return stores.Execution{}, fmt.Errorf("%w: snapshot executions cannot be retried", ErrNotRetryable)
	}
	if previous.Status != stores.StatusFailed {
		return stores.Execution{}, fmt.Errorf("%w: only failed executions can be retried", ErrNotRetryable)
	}
	if !hasRetryableSteps(previous.Steps) {
		return stores.Execution{}, fmt.Errorf("%w: execution has no failed or skipped steps", ErrNotRetryable)
	}

	workspaceID := previous.ID
	if previous.WorkspaceID != nil {
		workspaceID = *previous.WorkspaceID
	}
	dir := r.Workspace.RepoDir(project.ID, workspaceID)
	if _, err := os.Stat(dir); err != nil {
		return stores.Execution{}, fmt.Errorf("%w: %s", ErrWorkspaceGone, dir)
	}
	busy, err := r.Store.WorkspaceBusy(ctx, project.ID, workspaceID)
	if err != nil {
		return stores.Execution{}, err
	}
	if busy {
		return stores.Execution{}, fmt.Errorf("%w: another attempt is already running", ErrNotRetryable)
	}

	parentID := previous.ID
	return r.enqueue(ctx, stores.Execution{
		ProjectID:         project.ID,
		Project:           project.Name,
		ExecutionID:       previous.ExecutionID,
		Attempt:           previous.Attempt + 1,
		Workflow:          previous.Workflow,
		Ref:               previous.Ref,
		CommitSHA:         previous.CommitSHA,
		Trigger:           stores.TriggerRetry,
		Source:            stores.SourceGit,
		PreviousAttemptID: &parentID,
		WorkspaceID:       &workspaceID,
		CreatedAt:         time.Now(),
		CreatedBy:         createdBy,
	})
}

func hasRetryableSteps(steps stores.Steps) bool {
	for _, s := range steps {
		if s.Status == stores.StepStatusFailed || s.Status == stores.StepStatusSkipped {
			return true
		}
	}
	return false
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

func (r *Runner) run(ctx context.Context, exec stores.Execution) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	project, err := r.Store.GetProject(ctx, exec.ProjectID)
	if err != nil {
		r.fail(ctx, exec, err)
		return
	}
	notifier := notify.For(project, r.PublicBaseURL, r.Store)

	go r.watchCancel(ctx, cancel, project.ID, exec.ID)

	exec.Status = stores.StatusRunning
	_ = r.Store.UpdateExecution(ctx, exec)

	vars := r.loadVariables(ctx, project.ID)
	secrets := collectSecrets(vars, project)

	logDir := r.Workspace.LogDir(project.ID, exec.ID)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		r.fail(ctx, exec, err)
		return
	}
	setupFile, err := os.OpenFile(r.Workspace.SetupLog(project.ID, exec.ID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		r.fail(ctx, exec, err)
		return
	}
	setupLog := mask.NewWriter(setupFile, secrets)
	defer func() {
		_ = setupLog.Flush()
		setupFile.Close()
	}()

	fmt.Fprintf(setupLog, "pici: starting workflow %q on %s\n", exec.Workflow, exec.Ref)
	if r.Engine == nil {
		r.finish(ctx, exec, stores.StatusFailed, nil, "container engine is unavailable", notifier, setupLog)
		return
	}

	workspaceID := exec.ID
	if exec.WorkspaceID != nil {
		workspaceID = *exec.WorkspaceID
	}
	repoDir := r.Workspace.RepoDir(project.ID, workspaceID)
	isRetry := exec.Trigger == stores.TriggerRetry
	if isRetry {
		if _, err := os.Stat(repoDir); err != nil {
			fmt.Fprintf(setupLog, "workspace unavailable: %v\n", err)
			r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), notifier, setupLog)
			return
		}
	} else if err := r.materializeSource(ctx, project, exec, repoDir); err != nil {
		fmt.Fprintf(setupLog, "prepare source failed: %v\n", err)
		r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), notifier, setupLog)
		return
	}

	sha := resolveCommitSHA(repoDir)
	exec.CommitSHA = sha
	_ = r.Store.UpdateExecution(ctx, exec)

	wfDir := filepath.Join(repoDir, ".ci", exec.Workflow)
	cfgData, err := os.ReadFile(filepath.Join(wfDir, "ci.yml"))
	if err != nil {
		fmt.Fprintf(setupLog, "read ci.yml: %v\n", err)
		r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), notifier, setupLog)
		return
	}
	cfg, err := Parse(cfgData)
	if err != nil {
		fmt.Fprintf(setupLog, "parse ci.yml: %v\n", err)
		r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), notifier, setupLog)
		return
	}

	if exec.Trigger == stores.TriggerManual {
		isTag := exec.Source != stores.SourceSnapshot && git.IsTag(repoDir, exec.Ref)
		if !cfg.MatchesManual(exec.Ref, isTag) {
			msg := fmt.Sprintf("manual run of %q is not allowed on ref %q", exec.Workflow, exec.Ref)
			fmt.Fprintf(setupLog, "%s\n", msg)
			r.finish(ctx, exec, stores.StatusFailed, nil, msg, notifier, setupLog)
			return
		}
	}

	var parent stores.Execution
	var carry map[string]stores.StepResult
	if isRetry && exec.PreviousAttemptID != nil {
		if loaded, err := r.Store.GetAttempt(ctx, project.ID, *exec.PreviousAttemptID); err == nil {
			parent = loaded
			carry = carryOverSteps(loaded.Steps)
		}
	}

	// Persist the planned step order up front so the UI can name steps (and
	// their artifacts) as soon as the workflow is parsed, before the runner
	// image is built. On a retry, successful steps carry their parent result.
	if order, err := orderSteps(cfg.Steps); err == nil {
		planned := make(stores.Steps, len(order))
		for i, name := range order {
			if res, ok := carry[name]; ok {
				planned[i] = res
			} else {
				planned[i] = stores.StepResult{Name: name, Status: stores.StepStatusPending}
			}
		}
		exec.Steps = planned
		_ = r.Store.UpdateExecution(ctx, exec)

		if isRetry && exec.PreviousAttemptID != nil {
			for i, name := range order {
				if _, ok := carry[name]; !ok {
					continue
				}
				src := r.Workspace.StepLog(project.ID, *exec.PreviousAttemptID, stepIndexByName(parent.Steps, name), name)
				dst := r.Workspace.StepLog(project.ID, exec.ID, i, name)
				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
					_ = copyFile(src, dst)
				}
				r.collectArtifacts(project.ID, exec.ID, i, findStep(cfg.Steps, name).Artifacts, repoDir, setupLog)
			}
		}
	}

	if exec.Source != stores.SourceSnapshot {
		if err := SyncSchedule(ctx, r.Store, project.ID, exec.Workflow, cfg.EffectiveSchedule()); err != nil {
			fmt.Fprintf(setupLog, "schedule sync failed: %v\n", err)
		}
	}

	if cfg.Concurrency != "" {
		exec.ConcurrencyGroup = cfg.Concurrency
		_ = r.Store.UpdateExecution(ctx, exec)
		if err := r.Store.CancelRunningInGroup(ctx, project.ID, cfg.Concurrency, exec.ID); err != nil {
			fmt.Fprintf(setupLog, "concurrency cancel failed: %v\n", err)
		}
	}

	autoEnv, autoCache := detectCaches(repoDir, CacheMountPath)
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	for k, v := range autoEnv {
		if _, ok := cfg.Env[k]; !ok {
			cfg.Env[k] = v
		}
	}

	// Explicit caches are mounted under the repo (they are repo-relative by
	// design); auto-detected ones live in a dedicated path outside the repo.
	autoBinds := cacheBinds(project.ID.String(), CacheMountPath, autoCache)
	explicitBinds := cacheBinds(project.ID.String(), r.MountPath, cfg.Cache)
	cacheBinds := append(explicitBinds, autoBinds...)

	if err := notifier.Started(ctx, exec); err != nil {
		fmt.Fprintf(setupLog, "notify: %v\n", err)
	}

	image := cfg.Image
	if image == "" {
		image = imageTag(project.ID.String(), exec.Workflow)
		fmt.Fprintf(setupLog, "building image %s...\n", image)
		if err := r.Engine.Build(ctx, docker.BuildOptions{
			ContextDir: wfDir,
			Dockerfile: "Dockerfile",
			Tags:       []string{image},
			CacheFrom:  []string{image},
			Progress:   setupLog,
		}); err != nil {
			fmt.Fprintf(setupLog, "build failed: %v\n", err)
			r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), notifier, setupLog)
			return
		}
	}

	env := buildEnv(project, exec, r.MountPath, sha, resolveVersion(repoDir, exec.Ref, sha))
	for _, v := range vars {
		env = append(env, v.Key+"="+v.Value)
	}
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}

	exec.SetupFinishedAt = new(time.Now())
	// stepBase carries the execution as it is while steps run; the runner
	// persists each step transition so the UI can follow along live.
	stepBase := exec
	onStepUpdate := func(steps stores.Steps) {
		e := stepBase
		e.Steps = steps
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.Store.UpdateExecution(updateCtx, e)
	}
	steps, failed, canceled := r.executeSteps(ctx, cfg, env, image, repoDir, wfDir, project.ID, exec.ID, secrets, cacheBinds, carry, onStepUpdate)
	exec.Steps = steps
	exec.Error = stepError(steps)
	exec.FinishedAt = new(time.Now())

	status := stores.StatusSuccess
	if canceled || ctx.Err() != nil {
		status = stores.StatusCanceled
	} else if failed {
		status = stores.StatusFailed
	}
	exec.Status = status

	r.finish(ctx, exec, status, steps, exec.Error, notifier, setupLog)
}

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
	src := r.Workspace.Snapshot(project.ID, *exec.SnapshotID)
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return err
	}
	if err := archive.ExtractTarGz(f, repoDir, r.MaxSnapshotSize); err != nil {
		return err
	}

	// .git/config is excluded from the upload; recreate just the origin remote
	// from the project's own repo URL.
	configPath := filepath.Join(repoDir, ".git", "config")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	config := "[remote \"origin\"]\n\turl = " + strconv.Quote(project.RepoURL) + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		return err
	}

	return nil
}

func (r *Runner) watchCancel(ctx context.Context, cancel context.CancelFunc, projectID uuid.UUID, execID int64) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := r.Store.IsCancelRequested(context.Background(), projectID, execID)
			if err == nil && ok {
				cancel()
				return
			}
		}
	}
}

func (r *Runner) fail(ctx context.Context, exec stores.Execution, err error) {
	r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), notify.For(stores.Project{}, "", nil), nil)
}

func (r *Runner) finish(ctx context.Context, exec stores.Execution, status stores.Status, steps stores.Steps, errMsg string, notifier notify.Reporter, setupLog *mask.Writer) {
	exec.Status = status
	exec.Steps = steps
	exec.Error = errMsg
	if exec.FinishedAt == nil {
		exec.FinishedAt = new(time.Now())
	}
	// When setup fails before the steps run, all elapsed time was setup.
	if exec.SetupFinishedAt == nil && setupLog != nil {
		exec.SetupFinishedAt = exec.FinishedAt
	}

	persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = r.Store.UpdateExecution(persistCtx, exec)

	if err := notifier.Finished(ctx, notify.Report{
		Execution: exec,
		Status:    status,
		Summary:   errMsg,
		Logs:      func() string { return r.readLogs(exec.ProjectID, exec.ID) },
	}); err != nil && setupLog != nil {
		fmt.Fprintf(setupLog, "notify: %v\n", err)
	}

	// The execution is terminal: its snapshot archive is no longer needed and
	// can be dropped. GC also cleans up stale uploads as a backstop.
	if exec.Source == stores.SourceSnapshot && exec.SnapshotID != nil {
		_ = os.Remove(r.Workspace.Snapshot(exec.ProjectID, *exec.SnapshotID))
	}

	if setupLog != nil {
		fmt.Fprintf(setupLog, "pici: workflow %q finished with status %s\n", exec.Workflow, status)
	}
}

func (r *Runner) loadVariables(ctx context.Context, projectID uuid.UUID) []stores.Variable {
	global, _ := r.Store.ListVariables(ctx, nil)
	project, _ := r.Store.ListVariables(ctx, &projectID)
	return append(global, project...)
}

func collectSecrets(vars []stores.Variable, project stores.Project) []string {
	var out []string
	for _, v := range vars {
		if v.Secret && v.Value != "" {
			out = append(out, v.Value)
		}
	}
	if project.AuthSecret != "" {
		out = append(out, project.AuthSecret)
	}
	if project.WebhookSecret != "" {
		out = append(out, project.WebhookSecret)
	}
	return out
}

func carryOverSteps(steps stores.Steps) map[string]stores.StepResult {
	out := make(map[string]stores.StepResult)
	for _, s := range steps {
		if s.Status == stores.StepStatusSuccess {
			out[s.Name] = s
		}
	}
	return out
}

func (r *Runner) readLogs(projectID uuid.UUID, execID int64) string {
	const max = 60000
	logs := r.Workspace.ReadLogs(projectID, execID)
	defer logs.Close()
	data, _ := io.ReadAll(io.LimitReader(logs, max+1))
	s := string(data)
	if len(s) > max {
		s = s[:max] + "\n... (truncated)"
	}
	return s
}

func stepError(steps stores.Steps) string {
	for _, s := range steps {
		if s.Status == stores.StepStatusFailed {
			return fmt.Sprintf("step %q failed: %s", s.Name, s.Error)
		}
	}
	return ""
}

func buildEnv(project stores.Project, exec stores.Execution, mountPath, sha, version string) []string {
	source := exec.Source
	if source == "" {
		source = stores.SourceGit
	}
	return []string{
		"CI=true",
		"PICI=true",
		"PICI_SOURCE=" + string(source),
		"PICI_PROJECT=" + project.Name,
		"PICI_PROJECT_ID=" + project.ID.String(),
		"PICI_REPO_URL=" + project.RepoURL,
		"PICI_REPO_SLUG=" + github.RepoSlug(project.RepoURL),
		"PICI_WORKFLOW=" + exec.Workflow,
		"PICI_EXECUTION_ID=" + strconv.FormatInt(exec.ExecutionID, 10),
		"PICI_REF=" + exec.Ref,
		"PICI_VERSION=" + version,
		"PICI_COMMIT_SHA=" + sha,
		"PICI_REPO_DIR=" + mountPath,
		"PICI_WORKFLOW_DIR=" + mountPath + "/.ci/" + exec.Workflow,
		// Trust the mounted repo whatever its uid/gid, so git and go's VCS
		// stamping work without any per-workflow setup. The default identity
		// lets steps commit without configuring user.name/user.email.
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=safe.directory",
		"GIT_CONFIG_VALUE_0=*",
		"GIT_CONFIG_KEY_1=user.name",
		"GIT_CONFIG_VALUE_1=pici",
		"GIT_CONFIG_KEY_2=user.email",
		"GIT_CONFIG_VALUE_2=pici@localhost",
	}
}

func resolveVersion(dir, ref, sha string) string {
	if repo, err := gogit.PlainOpen(dir); err == nil {
		if _, err := repo.Reference(plumbing.NewTagReferenceName(ref), false); err == nil {
			return ref
		}
	}
	return present.SHA(sha)
}

func resolveCommitSHA(dir string) string {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return ""
	}
	head, err := repo.Head()
	if err != nil {
		return ""
	}
	return head.Hash().String()
}

func imageTag(projectID, workflow string) string {
	return ImagePrefix(projectID) + docker.CleanTags(workflow)
}

// ImagePrefix is the repository prefix shared by every workflow image built for
// a project. It is also used to find those images for the cache view.
func ImagePrefix(projectID string) string {
	return "pici/" + docker.CleanTags(projectID) + "-"
}
