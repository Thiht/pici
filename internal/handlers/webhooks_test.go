package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func newWebhookServer(t *testing.T, repoURL, webhookSecret string) (*Handler, stores.Store) {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	project := stores.Project{
		ID:            "proj-1",
		Name:          "demo",
		RepoURL:       repoURL,
		Provider:      stores.ProviderGitHub,
		AuthType:      stores.AuthTypeNone,
		WebhookSecret: webhookSecret,
		DefaultBranch: "master",
		CreatedAt:     time.Now().UnixMilli(),
		UpdatedAt:     time.Now().UnixMilli(),
	}
	if _, err := store.CreateProject(t.Context(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	runner := &ci.Runner{Store: store}
	s := New(store, runner, nil, t.TempDir(), "/workspace", "")
	return s, store
}

func makeRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	files["README.md"] = "# test\n"
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &gogit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	return dir, head.Hash().String()
}

func sendWebhook(t *testing.T, s *Handler, project, event, secret string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/"+project, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	return rr
}

func enqueuedWorkflows(t *testing.T, store stores.Store, projectID string) []string {
	t.Helper()
	execs, err := store.ListExecutions(t.Context(), projectID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range execs {
		out = append(out, e.Workflow)
	}
	return out
}

func TestWebhookSignatureRejected(t *testing.T) {
	dir, _ := makeRepo(t, map[string]string{
		".ci/build/ci.yml": "steps:\n  - name: a\n    run: echo hi\n",
	})
	s, _ := newWebhookServer(t, dir, "correct-secret")

	rr := sendWebhook(t, s, "demo", "ping", "wrong-secret", map[string]string{})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestWebhookPing(t *testing.T) {
	dir, _ := makeRepo(t, map[string]string{})
	s, _ := newWebhookServer(t, dir, "secret")

	rr := sendWebhook(t, s, "demo", "ping", "secret", map[string]string{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestWebhookPushAllWorkflows(t *testing.T) {
	dir, sha := makeRepo(t, map[string]string{
		".ci/build/ci.yml":  "steps:\n  - name: a\n    run: echo build\n",
		".ci/deploy/ci.yml": "steps:\n  - name: b\n    run: echo deploy\n",
	})
	s, store := newWebhookServer(t, dir, "")

	payload := map[string]any{
		"ref":   "refs/heads/main",
		"after": sha,
		"head_commit": map[string]any{
			"added":    []string{"src/main.go"},
			"modified": []string{},
			"removed":  []string{},
		},
	}
	rr := sendWebhook(t, s, "demo", "push", "", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	got := enqueuedWorkflows(t, store, "proj-1")
	if len(got) != 2 {
		t.Fatalf("expected 2 workflows enqueued, got %v", got)
	}
}

func TestWebhookPushPathFilter(t *testing.T) {
	dir, sha := makeRepo(t, map[string]string{
		".ci/build/ci.yml": "paths:\n  - src/**\nsteps:\n  - name: a\n    run: echo build\n",
		".ci/docs/ci.yml":  "paths:\n  - docs/**\nsteps:\n  - name: b\n    run: echo docs\n",
	})
	s, store := newWebhookServer(t, dir, "")

	payload := map[string]any{
		"ref":   "refs/heads/main",
		"after": sha,
		"head_commit": map[string]any{
			"added":    []string{"src/foo.go"},
			"modified": []string{},
			"removed":  []string{},
		},
	}
	rr := sendWebhook(t, s, "demo", "push", "", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	got := enqueuedWorkflows(t, store, "proj-1")
	if len(got) != 1 || got[0] != "build" {
		t.Fatalf("expected only build workflow, got %v", got)
	}
}

func TestWebhookPullRequest(t *testing.T) {
	dir, sha := makeRepo(t, map[string]string{
		".ci/build/ci.yml": "steps:\n  - name: a\n    run: echo build\n",
	})
	s, store := newWebhookServer(t, dir, "")

	payload := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 42,
			"head":   map[string]any{"ref": "feature", "sha": sha},
		},
	}
	rr := sendWebhook(t, s, "demo", "pull_request", "", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	execs, err := store.ListExecutions(t.Context(), "proj-1", 100)
	if err != nil || len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(execs), err)
	}
	if execs[0].Ref != "refs/pull/42/head" {
		t.Fatalf("expected PR ref, got %q", execs[0].Ref)
	}
	if execs[0].CommitSHA != sha {
		t.Fatalf("expected commit sha %s, got %s", sha, execs[0].CommitSHA)
	}
	if execs[0].Trigger != stores.TriggerWebhook {
		t.Fatalf("expected trigger webhook, got %q", execs[0].Trigger)
	}
}

func TestWebhookUnsupportedEvent(t *testing.T) {
	dir, _ := makeRepo(t, map[string]string{})
	s, _ := newWebhookServer(t, dir, "")

	rr := sendWebhook(t, s, "demo", "issues", "", map[string]string{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func sendGitLabWebhook(t *testing.T, s *Handler, project, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab/"+project, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", token)
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	return rr
}

func TestWebhookGitLabPush(t *testing.T) {
	dir, sha := makeRepo(t, map[string]string{
		".ci/build/ci.yml": "steps:\n  - name: a\n    run: echo build\n",
	})
	s, store := newWebhookServer(t, dir, "")

	payload := map[string]any{
		"object_kind": "push",
		"ref":         "refs/heads/main",
		"after":       sha,
		"commits": []map[string]any{
			{"added": []string{"src/main.go"}, "modified": []string{}, "removed": []string{}},
		},
	}
	rr := sendGitLabWebhook(t, s, "demo", "", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	execs, err := store.ListExecutions(t.Context(), "proj-1", 100)
	if err != nil || len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(execs), err)
	}
	if execs[0].CommitSHA != sha || execs[0].Trigger != stores.TriggerWebhook {
		t.Fatalf("unexpected execution: %+v", execs[0])
	}
}

func TestWebhookGitLabTokenRejected(t *testing.T) {
	dir, _ := makeRepo(t, map[string]string{})
	s, _ := newWebhookServer(t, dir, "gitlab-secret")

	payload := map[string]any{"object_kind": "push", "ref": "refs/heads/main", "after": "abc123"}
	rr := sendGitLabWebhook(t, s, "demo", "wrong-token", payload)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestWebhookGitLabMergeRequest(t *testing.T) {
	dir, sha := makeRepo(t, map[string]string{
		".ci/build/ci.yml": "steps:\n  - name: a\n    run: echo build\n",
	})
	s, store := newWebhookServer(t, dir, "")

	payload := map[string]any{
		"object_kind": "merge_request",
		"object_attributes": map[string]any{
			"iid":           7,
			"action":        "open",
			"source_branch": "feature",
			"last_commit":   map[string]any{"id": sha},
		},
	}
	rr := sendGitLabWebhook(t, s, "demo", "", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	execs, err := store.ListExecutions(t.Context(), "proj-1", 100)
	if err != nil || len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(execs), err)
	}
	if execs[0].Ref != "feature" || execs[0].CommitSHA != sha {
		t.Fatalf("unexpected execution: ref=%q sha=%q", execs[0].Ref, execs[0].CommitSHA)
	}
}
