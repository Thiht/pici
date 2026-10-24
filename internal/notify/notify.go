// Package notify reports an Execution's progress to its Project's provider:
// GitHub check runs, GitLab commit statuses, or nothing at all.
package notify

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"uuid"

	gh "github.com/google/go-github/v92/github"

	"github.com/Thiht/pici/internal/github"
	"github.com/Thiht/pici/internal/gitlab"
	"github.com/Thiht/pici/internal/stores"
)

// Store is the part of the store the GitHub reporter needs to keep one check
// run per Execution across Attempts.
type Store interface {
	GetExecutionAttempt(ctx context.Context, projectID uuid.UUID, executionID int64, attempt int) (stores.Execution, error)
	SetCheckRunID(ctx context.Context, projectID uuid.UUID, executionID, checkRunID int64) error
	ListAttempts(ctx context.Context, projectID uuid.UUID, executionID int64) ([]stores.Execution, error)
}

// Report is what a provider learns about an Attempt that reached a terminal
// status. Logs is lazy so a no-op reporter never reads log files.
type Report struct {
	Execution stores.Execution
	Status    stores.Status
	Summary   string
	Logs      func() string
}

// Reporter reports one Execution's progress. Each Reporter serves a single
// Execution, so an adapter keeps its own state without sharing it.
type Reporter interface {
	Started(ctx context.Context, execution stores.Execution) error
	Finished(ctx context.Context, report Report) error
}

// For returns the reporter for a Project, or one that does nothing when the
// Project has no provider to report to.
func For(project stores.Project, baseURL string, store Store) Reporter {
	if project.AuthSecret == "" {
		return nop{}
	}
	switch project.Provider {
	case stores.ProviderGithub:
		if owner, repo, ok := github.ParseRepo(project.RepoURL); ok {
			return &githubReporter{project: project, baseURL: baseURL, store: store, owner: owner, repo: repo}
		}
	case stores.ProviderGitlab:
		if _, _, ok := gitlab.ParseRepo(project.RepoURL); ok {
			return &gitlabReporter{project: project, baseURL: baseURL}
		}
	}
	return nop{}
}

type nop struct{}

func (nop) Started(context.Context, stores.Execution) error { return nil }
func (nop) Finished(context.Context, Report) error          { return nil }

// githubReporter keeps one check run per Execution: a retry updates the check
// run the first Attempt created instead of adding a second one to the commit.
type githubReporter struct {
	project    stores.Project
	baseURL    string
	store      Store
	owner      string
	repo       string
	checkRunID int64
}

func (r *githubReporter) Started(ctx context.Context, execution stores.Execution) error {
	if execution.Source == stores.SourceSnapshot || execution.CommitSHA == "" {
		return nil
	}
	root, err := r.store.GetExecutionAttempt(ctx, r.project.ID, execution.ExecutionID, 1)
	if err != nil {
		return fmt.Errorf("load github check run id: %w", err)
	}
	if root.CheckRunID == 0 {
		if root.CheckRunID, err = r.create(ctx, execution); err != nil {
			return err
		}
		if err := r.store.SetCheckRunID(ctx, r.project.ID, execution.ExecutionID, root.CheckRunID); err != nil {
			return fmt.Errorf("store github check run id: %w", err)
		}
	}
	r.checkRunID = root.CheckRunID

	client, err := gh.NewClient(gh.WithAuthToken(r.project.AuthSecret))
	if err != nil {
		return fmt.Errorf("mark github check run running: %w", err)
	}
	status := "in_progress"
	title := fmt.Sprintf("pici/%s: running (attempt %d/%d)", execution.Workflow, execution.Attempt, execution.AttemptsCount)
	summary := r.attempts(ctx, execution, fmt.Sprintf("Attempt %d of %d is running.", execution.Attempt, execution.AttemptsCount))
	_, _, err = client.Checks.UpdateCheckRun(ctx, r.owner, r.repo, r.checkRunID, gh.UpdateCheckRunOptions{
		Name:       "pici/" + execution.Workflow,
		Status:     &status,
		DetailsURL: detailsURL(r.baseURL, execution),
		Output: &gh.CheckRunOutput{
			Title:   &title,
			Summary: &summary,
		},
	})
	if err != nil {
		return fmt.Errorf("mark github check run running: %w", err)
	}
	return nil
}

func (r *githubReporter) Finished(ctx context.Context, report Report) error {
	if r.checkRunID == 0 {
		return nil
	}

	conclusion := "success"
	switch report.Status {
	case stores.StatusFailed:
		conclusion = "failure"
	case stores.StatusCanceled:
		conclusion = "cancelled"
	}
	title := fmt.Sprintf("pici/%s: %s (attempt %d/%d)", report.Execution.Workflow, conclusion, report.Execution.Attempt, report.Execution.AttemptsCount)
	summary := report.Summary
	if summary == "" {
		summary = "Workflow completed successfully"
	}
	summary = r.attempts(ctx, report.Execution, summary)
	logs := ""
	if report.Logs != nil {
		logs = report.Logs()
	}
	completed := "completed"

	client, err := gh.NewClient(gh.WithAuthToken(r.project.AuthSecret))
	if err != nil {
		return fmt.Errorf("update github check run: %w", err)
	}
	_, _, err = client.Checks.UpdateCheckRun(ctx, r.owner, r.repo, r.checkRunID, gh.UpdateCheckRunOptions{
		Name:        "pici/" + report.Execution.Workflow,
		Status:      &completed,
		Conclusion:  &conclusion,
		CompletedAt: &gh.Timestamp{Time: time.Now()},
		DetailsURL:  detailsURL(r.baseURL, report.Execution),
		Output: &gh.CheckRunOutput{
			Title:   &title,
			Summary: &summary,
			Text:    &logs,
		},
	})
	if err != nil {
		return fmt.Errorf("update github check run: %w", err)
	}
	return nil
}

func (r *githubReporter) create(ctx context.Context, execution stores.Execution) (int64, error) {
	client, err := gh.NewClient(gh.WithAuthToken(r.project.AuthSecret))
	if err != nil {
		return 0, fmt.Errorf("create github check run: %w", err)
	}
	status := "in_progress"
	checkRun, _, err := client.Checks.CreateCheckRun(ctx, r.owner, r.repo, gh.CreateCheckRunOptions{
		Name:       "pici/" + execution.Workflow,
		HeadSHA:    execution.CommitSHA,
		Status:     &status,
		StartedAt:  &gh.Timestamp{Time: time.Now()},
		DetailsURL: detailsURL(r.baseURL, execution),
	})
	if err != nil {
		return 0, fmt.Errorf("create github check run: %w", err)
	}
	return checkRun.GetID(), nil
}

// attempts appends the Attempt history to a check run summary, so a re-run
// shows what earlier Attempts did.
func (r *githubReporter) attempts(ctx context.Context, execution stores.Execution, summary string) string {
	attempts, err := r.store.ListAttempts(ctx, r.project.ID, execution.ExecutionID)
	if err != nil || len(attempts) == 0 {
		return summary
	}
	var b strings.Builder
	b.WriteString(summary)
	b.WriteString("\n\nAttempts:\n")
	for _, a := range attempts {
		fmt.Fprintf(&b, "- #%d: %s (%s)\n", a.Attempt, a.Status, a.Trigger)
	}
	return b.String()
}

type gitlabReporter struct {
	project stores.Project
	baseURL string
}

func (r *gitlabReporter) Started(ctx context.Context, execution stores.Execution) error {
	return r.status(ctx, execution, "running", "")
}

func (r *gitlabReporter) Finished(ctx context.Context, report Report) error {
	return r.status(ctx, report.Execution, gitlabState(report.Status), report.Summary)
}

func (r *gitlabReporter) status(ctx context.Context, execution stores.Execution, state, description string) error {
	if execution.Source == stores.SourceSnapshot || execution.CommitSHA == "" {
		return nil
	}
	base, path, ok := gitlab.ParseRepo(r.project.RepoURL)
	if !ok {
		return nil
	}
	var targetURL string
	if url := detailsURL(r.baseURL, execution); url != nil {
		targetURL = *url
	}
	if err := gitlab.SetCommitStatus(ctx, base, path, r.project.AuthSecret, execution.CommitSHA, state, "pici/"+execution.Workflow, targetURL, description); err != nil {
		return fmt.Errorf("update gitlab status: %w", err)
	}
	return nil
}

func gitlabState(status stores.Status) string {
	switch status {
	case stores.StatusSuccess:
		return "success"
	case stores.StatusFailed:
		return "failed"
	case stores.StatusCanceled:
		return "canceled"
	default:
		return "running"
	}
}

// detailsURL points at the Execution's page in the web UI. It is nil when no
// public URL is configured.
func detailsURL(baseURL string, execution stores.Execution) *string {
	if baseURL == "" {
		return nil
	}
	url := strings.TrimRight(baseURL, "/") + "/projects/" + execution.ProjectID.String() +
		"/executions/" + strconv.FormatInt(execution.ExecutionID, 10)
	return &url
}
