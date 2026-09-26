package ci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"uuid"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gh "github.com/google/go-github/v92/github"

	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/github"
	"github.com/Thiht/pici/internal/mask"
	"github.com/Thiht/pici/internal/stores"
)

const maxParallelSteps = 8

type Runner struct {
	Store              stores.Store
	Engine             *docker.Engine
	WorkspaceDir       string
	MountPath          string
	LogsDir            string
	DefaultStepTimeout time.Duration
	PublicBaseURL      string

	workerID string
	mu       sync.Mutex
	cancels  map[uuid.UUID]context.CancelFunc
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
		r.cancels = make(map[uuid.UUID]context.CancelFunc)
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

		r.wg.Add(1)
		runCtx, cancel := context.WithCancel(context.Background())
		r.registerCancel(exec.ID, cancel)
		r.run(runCtx, exec)
		cancel()
		r.unregisterCancel(exec.ID)
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

func (r *Runner) registerCancel(id uuid.UUID, cancel context.CancelFunc) {
	r.mu.Lock()
	r.cancels[id] = cancel
	r.mu.Unlock()
}

func (r *Runner) unregisterCancel(id uuid.UUID) {
	r.mu.Lock()
	delete(r.cancels, id)
	r.mu.Unlock()
}

func (r *Runner) Cancel(execID uuid.UUID) {
	r.mu.Lock()
	cancel := r.cancels[execID]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Runner) Enqueue(ctx context.Context, project stores.Project, workflow, ref, commitSHA string, trigger stores.Trigger) (stores.Execution, error) {
	exec := stores.Execution{
		ID:        uuid.New(),
		ProjectID: project.ID,
		Project:   project.Name,
		Workflow:  workflow,
		Ref:       ref,
		CommitSHA: commitSHA,
		Status:    stores.StatusPending,
		Trigger:   trigger,
		CreatedAt: time.Now(),
	}
	if err := r.Store.CreateExecution(ctx, exec); err != nil {
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

	go r.watchCancel(ctx, cancel, exec.ID)

	exec.Status = stores.StatusRunning
	_ = r.Store.UpdateExecution(ctx, exec)

	vars := r.loadVariables(ctx, project.ID)
	secrets := collectSecrets(vars, project)

	logDir := r.LogDir(exec.ID.String())
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		r.fail(ctx, exec, err)
		return
	}
	setupFile, err := os.OpenFile(r.SetupLogPath(exec.ID.String()), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
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

	repoDir := filepath.Join(r.WorkspaceDir, project.ID.String(), exec.ID.String())
	cloneRef := exec.Ref
	if exec.CommitSHA != "" {
		cloneRef = exec.CommitSHA
	}
	cloneCfg := git.CloneConfig{
		URL:  project.RepoURL,
		Dir:  repoDir,
		Ref:  cloneRef,
		Auth: git.Auth{Type: project.AuthType.String(), User: project.AuthUser, Secret: project.AuthSecret},
	}
	if err := git.Clone(ctx, cloneCfg); err != nil {
		fmt.Fprintf(setupLog, "clone failed: %v\n", err)
		r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), project, setupLog, 0)
		return
	}

	sha := resolveCommitSHA(repoDir)
	exec.CommitSHA = sha
	_ = r.Store.UpdateExecution(ctx, exec)

	wfDir := filepath.Join(repoDir, ".ci", exec.Workflow)
	cfgData, err := os.ReadFile(filepath.Join(wfDir, "ci.yml"))
	if err != nil {
		fmt.Fprintf(setupLog, "read ci.yml: %v\n", err)
		r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), project, setupLog, 0)
		return
	}
	cfg, err := Parse(cfgData)
	if err != nil {
		fmt.Fprintf(setupLog, "parse ci.yml: %v\n", err)
		r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), project, setupLog, 0)
		return
	}

	if err := SyncSchedule(ctx, r.Store, project.ID, exec.Workflow, cfg.Schedule); err != nil {
		fmt.Fprintf(setupLog, "schedule sync failed: %v\n", err)
	}

	if cfg.Concurrency != "" {
		exec.ConcurrencyGroup = cfg.Concurrency
		_ = r.Store.UpdateExecution(ctx, exec)
		if err := r.Store.CancelRunningInGroup(ctx, project.ID, cfg.Concurrency, exec.ID); err != nil {
			fmt.Fprintf(setupLog, "concurrency cancel failed: %v\n", err)
		}
	}

	autoEnv, autoCache := detectCaches(repoDir, r.MountPath)
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	for k, v := range autoEnv {
		if _, ok := cfg.Env[k]; !ok {
			cfg.Env[k] = v
		}
	}
	cfg.Cache = append(cfg.Cache, autoCache...)

	cacheBinds := cacheBinds(project.ID.String(), r.MountPath, cfg.Cache)

	checkRunID := r.createCheckRun(ctx, project, exec, setupLog)

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
			r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), project, setupLog, checkRunID)
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

	steps, failed, canceled := r.executeSteps(ctx, cfg, env, image, repoDir, wfDir, exec.ID.String(), secrets, cacheBinds)
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

	r.finish(ctx, exec, status, steps, exec.Error, project, setupLog, checkRunID)
}

func (r *Runner) watchCancel(ctx context.Context, cancel context.CancelFunc, execID uuid.UUID) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := r.Store.IsCancelRequested(context.Background(), execID)
			if err == nil && ok {
				cancel()
				return
			}
		}
	}
}

func (r *Runner) fail(ctx context.Context, exec stores.Execution, err error) {
	r.finish(ctx, exec, stores.StatusFailed, nil, err.Error(), stores.Project{}, nil, 0)
}

func (r *Runner) finish(_ context.Context, exec stores.Execution, status stores.Status, steps stores.Steps, errMsg string, project stores.Project, setupLog *mask.Writer, checkRunID int64) {
	exec.Status = status
	exec.Steps = steps
	exec.Error = errMsg
	if exec.FinishedAt == nil {
		exec.FinishedAt = new(time.Now())
	}

	persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = r.Store.UpdateExecution(persistCtx, exec)

	if project.ID != uuid.Nil() {
		r.updateCheckRun(persistCtx, project, exec, checkRunID, status, errMsg, setupLog)
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

func (r *Runner) createCheckRun(ctx context.Context, project stores.Project, exec stores.Execution, log io.Writer) int64 {
	if project.Provider != stores.ProviderGithub || project.AuthSecret == "" || exec.CommitSHA == "" {
		return 0
	}
	owner, repo, ok := github.ParseRepo(project.RepoURL)
	if !ok {
		return 0
	}

	client, err := gh.NewClient(gh.WithAuthToken(project.AuthSecret))
	if err != nil {
		if log != nil {
			fmt.Fprintf(log, "create github check run failed: %v\n", err)
		}
		return 0
	}
	status := "in_progress"
	checkRun, _, err := client.Checks.CreateCheckRun(ctx, owner, repo, gh.CreateCheckRunOptions{
		Name:       "pici/" + exec.Workflow,
		HeadSHA:    exec.CommitSHA,
		Status:     &status,
		StartedAt:  &gh.Timestamp{Time: time.Now()},
		DetailsURL: r.detailsURL(exec.ID.String()),
	})
	if err != nil {
		if log != nil {
			fmt.Fprintf(log, "create github check run failed: %v\n", err)
		}
		return 0
	}
	return checkRun.GetID()
}

func (r *Runner) updateCheckRun(ctx context.Context, project stores.Project, exec stores.Execution, checkRunID int64, status stores.Status, errMsg string, log io.Writer) {
	if checkRunID == 0 || project.Provider != stores.ProviderGithub || project.AuthSecret == "" {
		return
	}
	owner, repo, ok := github.ParseRepo(project.RepoURL)
	if !ok {
		return
	}

	conclusion := "success"
	switch status {
	case stores.StatusFailed:
		conclusion = "failure"
	case stores.StatusCanceled:
		conclusion = "cancelled"
	}

	title := "pici/" + exec.Workflow + ": " + conclusion
	summary := errMsg
	if summary == "" {
		summary = "Workflow completed successfully"
	}

	client, err := gh.NewClient(gh.WithAuthToken(project.AuthSecret))
	if err != nil {
		if log != nil {
			fmt.Fprintf(log, "update github check run failed: %v\n", err)
		}
		return
	}
	logs := r.readLogs(exec.ID.String())
	completed := "completed"
	_, _, err = client.Checks.UpdateCheckRun(ctx, owner, repo, checkRunID, gh.UpdateCheckRunOptions{
		Name:        "pici/" + exec.Workflow,
		Status:      &completed,
		Conclusion:  &conclusion,
		CompletedAt: &gh.Timestamp{Time: time.Now()},
		DetailsURL:  r.detailsURL(exec.ID.String()),
		Output: &gh.CheckRunOutput{
			Title:   &title,
			Summary: &summary,
			Text:    &logs,
		},
	})
	if err != nil && log != nil {
		fmt.Fprintf(log, "update github check run failed: %v\n", err)
	}
}

func (r *Runner) detailsURL(execID string) *string {
	if r.PublicBaseURL == "" {
		return nil
	}
	url := strings.TrimRight(r.PublicBaseURL, "/") + "/api/executions/" + execID
	return &url
}

func (r *Runner) readLogs(execID string) string {
	const max = 60000
	var b strings.Builder
	for _, path := range r.LogPaths(execID) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		b.Write(data)
		if b.Len() >= max {
			break
		}
	}
	s := b.String()
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
	return []string{
		"CI=true",
		"PICI=true",
		"PICI_PROJECT=" + project.Name,
		"PICI_PROJECT_ID=" + project.ID.String(),
		"PICI_REPO_URL=" + project.RepoURL,
		"PICI_REPO_SLUG=" + repoSlug(project.RepoURL),
		"PICI_WORKFLOW=" + exec.Workflow,
		"PICI_EXECUTION_ID=" + exec.ID.String(),
		"PICI_REF=" + exec.Ref,
		"PICI_VERSION=" + version,
		"PICI_COMMIT_SHA=" + sha,
		"PICI_REPO_DIR=" + mountPath,
		"PICI_WORKFLOW_DIR=" + mountPath + "/.ci/" + exec.Workflow,
		// Trust the mounted repo whatever its uid/gid, so git and go's VCS
		// stamping work without any per-workflow setup.
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=safe.directory",
		"GIT_CONFIG_VALUE_0=*",
	}
}

func repoSlug(repoURL string) string {
	s := strings.TrimSuffix(repoURL, ".git")
	switch {
	case strings.Contains(s, "://"):
		s = s[strings.Index(s, "://")+3:]
	case strings.Contains(s, "@") && strings.Contains(s, ":"):
		return strings.Trim(s[strings.Index(s, ":")+1:], "/")
	default:
		return strings.Trim(s, "/")
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.Trim(s, "/")
}

func resolveVersion(dir, ref, sha string) string {
	if repo, err := gogit.PlainOpen(dir); err == nil {
		if _, err := repo.Reference(plumbing.NewTagReferenceName(ref), false); err == nil {
			return ref
		}
	}
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
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
	return "pici/" + docker.CleanTags(projectID) + "-" + docker.CleanTags(workflow)
}
