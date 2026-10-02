package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v92/github"

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

type pushPayload struct {
	Ref     string       `json:"ref"`
	After   string       `json:"after"`
	Commits []pushCommit `json:"commits"`
}

type pushCommit struct {
	Added    []string `json:"added"`
	Removed  []string `json:"removed"`
	Modified []string `json:"modified"`
}

type pullRequestPayload struct {
	Action string `json:"action"`
	PR     struct {
		Number int `json:"number"`
		Head   struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
}

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
		if !verifySignature(project.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
			render.Error(w, http.StatusUnauthorized, errors.New("invalid signature"))
			return
		}
	}

	switch r.Header.Get("X-GitHub-Event") {
	case "ping":
		render.JSON(w, http.StatusOK, map[string]string{"status": "pong"})
	case "push":
		h.handlePush(r.Context(), w, project, body)
	case "pull_request":
		h.handlePullRequest(r.Context(), w, project, body)
	default:
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}

func (h *WebhooksHandler) handlePush(ctx context.Context, w http.ResponseWriter, project stores.Project, body []byte) {
	var payload pushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	// Every commit of the push counts for path filters: head_commit alone only
	// describes the last one.
	var changed []string
	for _, c := range payload.Commits {
		changed = append(changed, c.Added...)
		changed = append(changed, c.Modified...)
		changed = append(changed, c.Removed...)
	}
	isTag := strings.HasPrefix(payload.Ref, "refs/tags/")
	h.trigger(ctx, w, project, "push", normalizeRef(payload.Ref), payload.After, "", changed, isTag)
}

func (h *WebhooksHandler) handlePullRequest(ctx context.Context, w http.ResponseWriter, project stores.Project, body []byte) {
	var payload pullRequestPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	if payload.Action != "opened" && payload.Action != "synchronize" && payload.Action != "reopened" {
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	number := payload.PR.Number
	ref := "refs/pull/" + strconv.Itoa(number) + "/head"
	sha := payload.PR.Head.SHA

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

	h.trigger(ctx, w, project, "pull_request", ref, sha, payload.PR.Base.Ref, changed, false)
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

func verifySignature(secret string, body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

type gitlabPush struct {
	ObjectKind string `json:"object_kind"`
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Commits    []struct {
		Added    []string `json:"added"`
		Modified []string `json:"modified"`
		Removed  []string `json:"removed"`
	} `json:"commits"`
}

type gitlabMergeRequest struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID          int    `json:"iid"`
		Action       string `json:"action"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		LastCommit   struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
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
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Gitlab-Token")), []byte(project.WebhookSecret)) != 1 {
			render.Error(w, http.StatusUnauthorized, errors.New("invalid token"))
			return
		}
	}

	var kind struct {
		ObjectKind string `json:"object_kind"`
	}
	if err := json.Unmarshal(body, &kind); err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}

	switch kind.ObjectKind {
	case "push", "tag_push":
		var payload gitlabPush
		if err := json.Unmarshal(body, &payload); err != nil {
			render.Error(w, http.StatusBadRequest, err)
			return
		}
		var changed []string
		for _, c := range payload.Commits {
			changed = append(changed, c.Added...)
			changed = append(changed, c.Modified...)
			changed = append(changed, c.Removed...)
		}
		h.trigger(r.Context(), w, project, "push", normalizeRef(payload.Ref), payload.After, "", changed, kind.ObjectKind == "tag_push")
	case "merge_request":
		var payload gitlabMergeRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			render.Error(w, http.StatusBadRequest, err)
			return
		}
		attrs := payload.ObjectAttributes
		switch attrs.Action {
		case "open", "reopen", "update":
			var changed []string
			if project.AuthSecret != "" {
				if base, path, ok := gitlab.ParseRepo(project.RepoURL); ok {
					if files, err := gitlab.MergeRequestDiffs(r.Context(), base, path, project.AuthSecret, attrs.IID); err == nil {
						changed = files
					}
				}
			}
			h.trigger(r.Context(), w, project, "pull_request", attrs.SourceBranch, attrs.LastCommit.ID, attrs.TargetBranch, changed, false)
		default:
			render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		}
	default:
		render.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
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
