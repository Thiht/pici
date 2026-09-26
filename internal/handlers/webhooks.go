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
	"uuid"

	gh "github.com/google/go-github/v92/github"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/github"
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
	Ref        string `json:"ref"`
	After      string `json:"after"`
	HeadCommit *struct {
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	} `json:"head_commit"`
}

type pullRequestPayload struct {
	Action string `json:"action"`
	PR     struct {
		Number int `json:"number"`
		Head   struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
}

func (h *WebhooksHandler) GitHub(w http.ResponseWriter, r *http.Request) {
	idOrName := r.PathValue("id")
	var project stores.Project
	if id, err := uuid.Parse(idOrName); err == nil {
		p, err := h.store.GetProject(r.Context(), id)
		if err == nil {
			project = p
		} else if !errors.Is(err, stores.ErrNotFound) {
			render.Error(w, http.StatusInternalServerError, err)
			return
		}
	}
	if project.ID == uuid.Nil() {
		p, err := h.store.GetProjectByName(r.Context(), idOrName)
		if err != nil {
			if errors.Is(err, stores.ErrNotFound) {
				render.Error(w, http.StatusNotFound, err)
			} else {
				render.Error(w, http.StatusInternalServerError, err)
			}
			return
		}
		project = p
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
	var changed []string
	if payload.HeadCommit != nil {
		changed = append(changed, payload.HeadCommit.Added...)
		changed = append(changed, payload.HeadCommit.Modified...)
		changed = append(changed, payload.HeadCommit.Removed...)
	}
	h.trigger(ctx, w, project, normalizeRef(payload.Ref), payload.After, changed)
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

	h.trigger(ctx, w, project, ref, sha, changed)
}

func (h *WebhooksHandler) trigger(ctx context.Context, w http.ResponseWriter, project stores.Project, ref, sha string, changed []string) {
	dir := filepath.Join(h.workspaceDir, project.ID.String(), "_discovery")
	cloneRef := ref
	if sha != "" {
		cloneRef = sha
	}
	cloneCfg := git.CloneConfig{
		URL:  project.RepoURL,
		Dir:  dir,
		Ref:  cloneRef,
		Auth: git.Auth{Type: project.AuthType.String(), User: project.AuthUser, Secret: project.AuthSecret},
	}
	workflows, err := ci.DiscoverWorkflows(ctx, cloneCfg)
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
		if !ci.MatchesPaths(cfg.Paths, cfg.PathsIgnore, changed) {
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
		LastCommit   struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

func (h *WebhooksHandler) GitLab(w http.ResponseWriter, r *http.Request) {
	idOrName := r.PathValue("id")
	var project stores.Project
	if id, err := uuid.Parse(idOrName); err == nil {
		p, err := h.store.GetProject(r.Context(), id)
		if err == nil {
			project = p
		} else if !errors.Is(err, stores.ErrNotFound) {
			render.Error(w, http.StatusInternalServerError, err)
			return
		}
	}
	if project.ID == uuid.Nil() {
		p, err := h.store.GetProjectByName(r.Context(), idOrName)
		if err != nil {
			if errors.Is(err, stores.ErrNotFound) {
				render.Error(w, http.StatusNotFound, err)
			} else {
				render.Error(w, http.StatusInternalServerError, err)
			}
			return
		}
		project = p
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
		h.trigger(r.Context(), w, project, normalizeRef(payload.Ref), payload.After, changed)
	case "merge_request":
		var payload gitlabMergeRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			render.Error(w, http.StatusBadRequest, err)
			return
		}
		switch payload.ObjectAttributes.Action {
		case "open", "reopen", "update":
			h.trigger(r.Context(), w, project, payload.ObjectAttributes.SourceBranch, payload.ObjectAttributes.LastCommit.ID, nil)
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
