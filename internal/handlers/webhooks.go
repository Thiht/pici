package handlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v92/github"
	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/github"
	"github.com/Thiht/pici/internal/gitlab"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type WebhooksHandler struct {
	store        stores.Store
	runner       *ci.Runner
	workspaceDir string
}

func NewWebhooksHandler(store stores.Store, runner *ci.Runner, workspaceDir string) *WebhooksHandler {
	return &WebhooksHandler{store: store, runner: runner, workspaceDir: workspaceDir}
}

// zeroSHA is the "after" SHA GitHub and GitLab send when a push deletes a ref.
const zeroSHA = "0000000000000000000000000000000000000000"

func (h *WebhooksHandler) GitHub(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}

	if project.WebhookSecret != "" {
		signature := r.Header.Get(gh.SHA256SignatureHeader)
		if err := gh.ValidateSignature(signature, body, []byte(project.WebhookSecret)); err != nil {
			render.Error(w, http.StatusUnauthorized, errors.New("invalid signature"))
			return
		}
	}

	eventType := gh.WebHookType(r)
	switch eventType {
	case "ping":
		render.JSON(w, http.StatusOK, map[string]string{"status": "pong"})
		return
	case "push", "pull_request":
	default:
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	event, err := gh.ParseWebHook(eventType, body)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}

	switch e := event.(type) {
	case *gh.PushEvent:
		h.handlePush(r.Context(), w, project, e)
	case *gh.PullRequestEvent:
		h.handlePullRequest(r.Context(), w, project, e)
	}
}

func (h *WebhooksHandler) handlePush(ctx context.Context, w http.ResponseWriter, project stores.Project, payload *gh.PushEvent) {
	// A deleted branch or tag has no SHA to check out.
	if payload.GetDeleted() || payload.GetAfter() == zeroSHA {
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	// Every commit of the push counts for path filters: head_commit alone only
	// describes the last one.
	var changed []string
	for _, c := range payload.GetCommits() {
		changed = append(changed, c.GetAdded()...)
		changed = append(changed, c.GetModified()...)
		changed = append(changed, c.GetRemoved()...)
	}
	ref := payload.GetRef()
	h.trigger(ctx, w, project, "push", normalizeRef(ref), payload.GetAfter(), "", changed, strings.HasPrefix(ref, "refs/tags/"))
}

func (h *WebhooksHandler) handlePullRequest(ctx context.Context, w http.ResponseWriter, project stores.Project, payload *gh.PullRequestEvent) {
	switch payload.GetAction() {
	case "opened", "synchronize", "reopened":
	default:
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	// A pull request from a fork runs untrusted code with the project's
	// secrets, so it is never triggered automatically: the repository owner
	// runs it by hand. An unknown head repository (a deleted fork) is treated
	// as a fork as well.
	pullRequest := payload.GetPullRequest()
	if head := pullRequest.GetHead().GetRepo().GetFullName(); head == "" || head != pullRequest.GetBase().GetRepo().GetFullName() {
		slog.InfoContext(ctx, "skipped pull request from a fork", "project", project.Name, "number", payload.GetNumber(), "head_repo", head)
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "pull request from a fork"})
		return
	}

	number := payload.GetNumber()
	ref := "refs/pull/" + strconv.Itoa(number) + "/head"
	sha := pullRequest.GetHead().GetSHA()

	var changed []string
	if project.AuthSecret != "" {
		if owner, repo, ok := github.ParseRepo(project.RepoURL); ok {
			if client, err := gh.NewClient(gh.WithAuthToken(project.AuthSecret)); err == nil {
				if files, _, err := client.PullRequests.ListFiles(ctx, owner, repo, number, nil); err == nil {
					for _, f := range files {
						changed = append(changed, f.GetFilename())
					}
				}
			}
		}
	}

	h.trigger(ctx, w, project, "pull_request", ref, sha, pullRequest.GetBase().GetRef(), changed, false)
}

func (h *WebhooksHandler) trigger(ctx context.Context, w http.ResponseWriter, project stores.Project, event, ref, sha, baseBranch string, changed []string, isTag bool) {
	dir := filepath.Join(h.workspaceDir, project.ID.String(), "_discovery")
	cloneRef := ref
	if sha != "" {
		cloneRef = sha
	}

	defaultBranch := project.DefaultBranch
	if defaultBranch == "" {
		if refs, err := git.ListRefs(ctx, project.RepoURL, git.Auth{
			Type:   project.AuthType.String(),
			User:   project.AuthUser,
			Secret: project.AuthSecret,
		}); err == nil {
			defaultBranch = refs.Head
		}
	}

	workflows, err := ci.DiscoverProjectWorkflows(ctx, project, dir, cloneRef)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}

	ciDir := filepath.Join(dir, ".ci")
	var enqueued []string
	for _, wf := range workflows {
		data, err := os.ReadFile(filepath.Join(ciDir, wf, "ci.yml"))
		if err != nil {
			continue
		}
		cfg, err := ci.Parse(data)
		if err != nil {
			continue
		}
		var matches bool
		if event == "pull_request" {
			matches = cfg.MatchesPullRequest(defaultBranch, baseBranch, changed)
		} else {
			matches = cfg.MatchesPush(defaultBranch, ref, isTag, changed)
		}
		if !matches {
			continue
		}
		if _, err := h.runner.Enqueue(ctx, project, wf, ref, sha, stores.TriggerWebhook); err == nil {
			enqueued = append(enqueued, wf)
		}
	}
	render.JSON(w, http.StatusOK, map[string]any{"status": "accepted", "workflows": enqueued})
}

func (h *WebhooksHandler) GitLab(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}

	if project.WebhookSecret != "" {
		if subtle.ConstantTimeCompare([]byte(gl.HookEventToken(r)), []byte(project.WebhookSecret)) != 1 {
			render.Error(w, http.StatusUnauthorized, errors.New("invalid token"))
			return
		}
	}

	eventType := gl.WebhookEventType(r)
	switch eventType {
	case gl.EventTypePush, gl.EventTypeTagPush, gl.EventTypeMergeRequest:
	default:
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	event, err := gl.ParseWebhook(eventType, body)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}

	switch e := event.(type) {
	case *gl.PushEvent:
		var changed []string
		for _, c := range e.Commits {
			changed = append(changed, c.Added...)
			changed = append(changed, c.Modified...)
			changed = append(changed, c.Removed...)
		}
		h.triggerPush(r.Context(), w, project, e.Ref, e.After, changed, false)
	case *gl.TagEvent:
		var changed []string
		for _, c := range e.Commits {
			changed = append(changed, c.Added...)
			changed = append(changed, c.Modified...)
			changed = append(changed, c.Removed...)
		}
		h.triggerPush(r.Context(), w, project, e.Ref, e.After, changed, true)
	case *gl.MergeEvent:
		h.handleMergeRequest(r.Context(), w, project, e)
	default:
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}

// triggerPush runs the workflows of a GitLab push or tag push. Every commit of
// the push counts for path filters.
func (h *WebhooksHandler) triggerPush(ctx context.Context, w http.ResponseWriter, project stores.Project, ref, after string, changed []string, isTag bool) {
	// A deleted branch or tag has no SHA to check out.
	if after == zeroSHA {
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	h.trigger(ctx, w, project, "push", normalizeRef(ref), after, "", changed, isTag)
}

func (h *WebhooksHandler) handleMergeRequest(ctx context.Context, w http.ResponseWriter, project stores.Project, payload *gl.MergeEvent) {
	attrs := payload.ObjectAttributes
	switch attrs.Action {
	case "open", "reopen", "update":
	default:
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	// A merge request from a fork runs untrusted code with the project's
	// secrets, so it is never triggered automatically: the repository owner
	// runs it by hand. Missing project ids are treated as a fork as well.
	if attrs.SourceProjectID == 0 || attrs.SourceProjectID != attrs.TargetProjectID {
		slog.InfoContext(ctx, "skipped merge request from a fork", "project", project.Name, "iid", attrs.IID, "source_project_id", attrs.SourceProjectID)
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "merge request from a fork"})
		return
	}

	var changed []string
	if project.AuthSecret != "" {
		if base, path, ok := gitlab.ParseRepo(project.RepoURL); ok {
			if files, err := gitlab.MergeRequestDiffs(ctx, base, path, project.AuthSecret, int(attrs.IID)); err == nil {
				changed = files
			}
		}
	}

	h.trigger(ctx, w, project, "pull_request", attrs.SourceBranch, attrs.LastCommit.ID, attrs.TargetBranch, changed, false)
}

func normalizeRef(ref string) string {
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		return strings.TrimPrefix(ref, "refs/heads/")
	case strings.HasPrefix(ref, "refs/tags/"):
		return strings.TrimPrefix(ref, "refs/tags/")
	default:
		return ref
	}
}
