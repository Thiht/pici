package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
)

func TestForNoopWithoutProviderOrSecret(t *testing.T) {
	// A nil store would panic if a reporter tried to persist anything.
	for _, project := range []stores.Project{
		{Provider: stores.ProviderGeneric, AuthSecret: "tok", RepoURL: "https://example.com/acme/demo.git"},
		{Provider: stores.ProviderGithub, RepoURL: "https://github.com/acme/demo.git"},
		{Provider: stores.ProviderGithub, AuthSecret: "tok", RepoURL: "not a repo"},
	} {
		reporter := For(project, "https://pici.test", nil)
		if err := reporter.Started(context.Background(), stores.Execution{}); err != nil {
			t.Fatalf("Started: %v", err)
		}
		if err := reporter.Finished(context.Background(), Report{}); err != nil {
			t.Fatalf("Finished: %v", err)
		}
	}
}

func TestGitLabReports(t *testing.T) {
	type request struct {
		path, state, targetURL string
	}
	var requests []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State     string `json:"state"`
			TargetURL string `json:"target_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		requests = append(requests, request{path: r.URL.EscapedPath(), state: body.State, targetURL: body.TargetURL})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"status":"running"}`))
	}))
	defer srv.Close()

	project := stores.Project{
		ID: uuid.New(), Provider: stores.ProviderGitlab, AuthSecret: "tok",
		RepoURL: srv.URL + "/acme/demo.git",
	}
	execution := stores.Execution{
		ProjectID: project.ID, ExecutionID: 7, Attempt: 2, AttemptsCount: 2,
		Workflow: "build", CommitSHA: "abc123",
	}

	reporter := For(project, "https://pici.test/", nil)
	if err := reporter.Started(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if err := reporter.Finished(context.Background(), Report{Execution: execution, Status: stores.StatusFailed, Summary: "step failed"}); err != nil {
		t.Fatal(err)
	}

	if len(requests) != 2 {
		t.Fatalf("expected two status posts, got %+v", requests)
	}
	if requests[0].state != "running" || requests[1].state != "failed" {
		t.Fatalf("unexpected states: %+v", requests)
	}
	if requests[0].path != "/api/v4/projects/acme%2Fdemo/statuses/abc123" {
		t.Fatalf("unexpected path %q", requests[0].path)
	}
	want := "https://pici.test/projects/" + project.ID.String() + "/executions/7"
	if requests[0].targetURL != want {
		t.Fatalf("target_url = %q, want %q", requests[0].targetURL, want)
	}
}

func TestGitLabSkipsSnapshotAndMissingCommit(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	project := stores.Project{
		ID: uuid.New(), Provider: stores.ProviderGitlab, AuthSecret: "tok",
		RepoURL: srv.URL + "/acme/demo.git",
	}
	reporter := For(project, "", nil)
	for _, execution := range []stores.Execution{
		{Workflow: "build", Source: stores.SourceSnapshot, CommitSHA: "abc123"},
		{Workflow: "build", CommitSHA: ""},
	} {
		if err := reporter.Started(context.Background(), execution); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 0 {
		t.Fatalf("expected no commit statuses, got %d", requests)
	}
}

func TestDetailsURL(t *testing.T) {
	execution := stores.Execution{ProjectID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), ExecutionID: 3}
	if got := detailsURL("", execution); got != nil {
		t.Fatalf("expected no URL without a public base URL, got %q", *got)
	}
	got := detailsURL("https://pici.test/", execution)
	want := "https://pici.test/projects/11111111-1111-1111-1111-111111111111/executions/3"
	if got == nil || *got != want {
		t.Fatalf("detailsURL = %v, want %q", got, want)
	}
}
